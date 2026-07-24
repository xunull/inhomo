package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL 是 Anthropic Messages API 的官方地址；BaseURL 留空即用它（测试可指向 stub）。
const DefaultBaseURL = "https://api.anthropic.com"

// anthropicVersion 是 Messages API 要求的版本头。
const anthropicVersion = "2023-06-01"

// Anthropic 用 Anthropic Messages API 实现 Provider。
type Anthropic struct {
	APIKey    string
	Model     string
	BaseURL   string // 空 → DefaultBaseURL；测试注入 stub
	MaxTokens int
	http      *http.Client
}

// NewAnthropic 建一个 Anthropic provider（默认 1024 max_tokens、60s 超时）。
func NewAnthropic(apiKey, model string) *Anthropic {
	return &Anthropic{
		APIKey:    apiKey,
		Model:     model,
		MaxTokens: 1024,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
}

// Generate 用一条 user 消息调 /v1/messages，返回拼接的文本内容块。非 200 → 带上响应体（截断）报错。
func (a *Anthropic) Generate(ctx context.Context, prompt string) (string, error) {
	base := a.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	body, err := json.Marshal(map[string]any{
		"model":      a.Model,
		"max_tokens": a.MaxTokens,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", a.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := a.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("Anthropic API 返回 %s：%s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), nil
}
