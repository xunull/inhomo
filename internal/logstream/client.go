// Package logstream 连接 mihomo external-controller 的 /logs 流。
//
// mihomo 的 /logs 支持普通 HTTP GET 流式返回（换行分隔的 JSON，逐条 flush），
// 无需 websocket，因此这里只用标准库：net/http 拉流 + json.Decoder 逐条解码。
package logstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LogMessage 是 /logs 每条消息的结构：{"type": "...", "payload": "..."}。
// Payload 才是要送去解析的日志行文本（见后续工单 T02）。
type LogMessage struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// ConnSnapshot 是 GET /connections 的返回（只取本项目需要的字段）：当前活跃连接的快照。
type ConnSnapshot struct {
	Connections []Conn `json:"connections"`
}

// Conn 是一条活跃连接。upload/download 是该连接**累计**字节；chains[0] 是实际出境节点。
type Conn struct {
	ID       string    `json:"id"`
	Upload   int64     `json:"upload"`
	Download int64     `json:"download"`
	Start    time.Time `json:"start"`
	Chains   []string  `json:"chains"`
	Metadata ConnMeta  `json:"metadata"`
}

// ConnMeta 是连接元数据（destinationPort 在 mihomo 里是字符串）。
type ConnMeta struct {
	Network         string `json:"network"`
	Host            string `json:"host"`
	Process         string `json:"process"`
	DestinationPort string `json:"destinationPort"`
}

// ErrAuth 表示鉴权失败（secret 不正确）——不可重试的致命错误。
var ErrAuth = errors.New("鉴权失败：external-controller secret 不正确")

// newGET 建一个到 c.BaseURL+path 的 GET 请求并带上 secret 鉴权头（若有）。
// /connections、/version、/logs 三处 GET 调用点共用，避免各自重复拼鉴权头。
func (c *Client) newGET(ctx context.Context, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	return req, nil
}

// FetchConnections 拉取一次 /connections 快照（普通 GET，复用同一 http 传输/鉴权）。
func (c *Client) FetchConnections(ctx context.Context) (*ConnSnapshot, error) {
	req, err := c.newGET(ctx, "/connections")
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrAuth
	default:
		return nil, fmt.Errorf("/connections 返回意外状态 %s", resp.Status)
	}
	var snap ConnSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// Alive 探测该控制器是否可用：GET /version 返回 200 即视为可用（端点在跑；若开了鉴权，则所带 secret 也被接受）。
// 供 CLI 层零参数自动发现的探活用（见 internal/cli/discover.go）：带上 secret（若有）、复用同一
// TCP / unix socket 传输，超时由传入的 ctx 控制（本机/socket 探活近乎瞬时，超时只在不可达时才吃满）。
func (c *Client) Alive(ctx context.Context) bool {
	req, err := c.newGET(ctx, "/version")
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Client 连接单个 mihomo external-controller。
type Client struct {
	BaseURL string // 归一化后形如 http://127.0.0.1:9090
	Secret  string

	// 重连退避参数（包内固定，暂无对外调优需求）。
	initialBackoff time.Duration
	maxBackoff     time.Duration

	// 可选 UI 钩子：库本身不打印，交给调用方渲染。
	OnConnect   func()
	OnReconnect func(wait time.Duration)

	http *http.Client
}

// New 返回一个 Client。controller 支持两种形式：
//   - TCP：  "127.0.0.1:9090" / "http://127.0.0.1:9090"
//   - Unix： "unix:///tmp/verge/verge-mihomo.sock"（Clash Verge Rev 等只开 socket 的场景）
func New(controller, secret string) *Client {
	c := &Client{
		Secret:         secret,
		initialBackoff: 1 * time.Second,
		maxBackoff:     30 * time.Second,
	}
	if path, ok := unixSocketPath(controller); ok {
		// Unix socket：请求 URL 用占位 host，实际由 Transport 拨到 socket。
		c.BaseURL = "http://localhost"
		c.http = &http.Client{Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", path)
			},
		}}
		return c
	}
	base := controller
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	c.BaseURL = strings.TrimRight(base, "/")
	c.http = &http.Client{} // 流式请求，不设整体超时
	return c
}

// unixSocketPath 识别 "unix:///path" / "unix:/path" 形式，返回 socket 文件路径。
func unixSocketPath(controller string) (string, bool) {
	for _, pfx := range []string{"unix://", "unix:"} {
		if strings.HasPrefix(controller, pfx) {
			path := strings.TrimPrefix(controller, pfx)
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			return path, true
		}
	}
	return "", false
}

// Run 持续订阅日志流：
//   - 从未连上就出错（不可达 / 地址错）：视为配置问题，快速失败并返回。
//   - 鉴权失败：始终致命，不重试。
//   - 曾经连上后中途断开：指数退避自动重连，直到 ctx 取消。
func (c *Client) Run(ctx context.Context, level string, handle func(LogMessage)) error {
	backoff := c.initialBackoff
	everConnected := false
	for {
		connected, err := c.stream(ctx, level, handle)
		if connected {
			everConnected = true
			backoff = c.initialBackoff // 连上后重置退避
		}

		switch {
		case ctx.Err() != nil:
			return nil // 用户主动退出
		case errors.Is(err, ErrAuth):
			return err // 致命
		case !everConnected:
			return fmt.Errorf("无法连接 %s：%w（请确认 external-controller 已开启且地址/secret 正确）", c.BaseURL, err)
		}

		// 曾经连上、现在断开：退避后重连。
		if c.OnReconnect != nil {
			c.OnReconnect(backoff)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > c.maxBackoff {
			backoff = c.maxBackoff
		}
	}
}

// stream 建立一次连接并阻塞式地把每条日志交给 handle，直到出错或 ctx 取消。
// 返回的 connected 表示本次是否已成功连上（HTTP 200）——用于区分"配置问题"与"中途断开"。
func (c *Client) stream(ctx context.Context, level string, handle func(LogMessage)) (connected bool, err error) {
	path := "/logs"
	if q := (url.Values{"level": {level}}).Encode(); level != "" {
		path += "?" + q
	}
	req, err := c.newGET(ctx, path)
	if err != nil {
		return false, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// 连上了。
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, ErrAuth
	default:
		return false, fmt.Errorf("/logs 返回意外状态 %s", resp.Status)
	}

	if c.OnConnect != nil {
		c.OnConnect()
	}

	// /logs 是换行分隔的 JSON 流；json.Decoder 会逐条读出。
	dec := json.NewDecoder(resp.Body)
	for {
		var msg LogMessage
		if err := dec.Decode(&msg); err != nil {
			if ctx.Err() != nil {
				return true, nil // 主动退出，不算错误
			}
			return true, err // 含 io.EOF：视为断开，交给上层重连
		}
		handle(msg)
	}
}
