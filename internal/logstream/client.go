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

// ErrAuth 表示鉴权失败（secret 不正确）——不可重试的致命错误。
var ErrAuth = errors.New("鉴权失败：external-controller secret 不正确")

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
	u := c.BaseURL + "/logs"
	if q := (url.Values{"level": {level}}).Encode(); level != "" {
		u += "?" + q
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
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
