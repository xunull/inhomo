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

// DefaultOpenAIBaseURL 是 OpenAI 官方基址；DeepSeek 等兼容平台用各自基址（如 https://api.deepseek.com）。
// 约定同 OpenAI SDK 的 base_url：路径 /chat/completions 由本 provider 追加，是否带 /v1 由基址自身决定。
const DefaultOpenAIBaseURL = "https://api.openai.com/v1"

// OpenAI 用 OpenAI 兼容的 Chat Completions API 实现 Provider，可对接 OpenAI / DeepSeek / Groq / 本地 Ollama 等。
type OpenAI struct {
	APIKey  string
	Model   string
	BaseURL string // 空 → DefaultOpenAIBaseURL；DeepSeek 用 https://api.deepseek.com
	http    *http.Client
}

// NewOpenAI 建一个 OpenAI 兼容 provider（60s 超时）。
func NewOpenAI(apiKey, model string) *OpenAI {
	return &OpenAI{APIKey: apiKey, Model: model, http: &http.Client{Timeout: 60 * time.Second}}
}

// Generate 用一条 user 消息调 {base}/chat/completions（Bearer 鉴权），返回首个 choice 的消息内容。
func (o *OpenAI) Generate(ctx context.Context, prompt string) (string, error) {
	base := o.BaseURL
	if base == "" {
		base = DefaultOpenAIBaseURL
	}
	body, err := json.Marshal(map[string]any{
		"model":    o.Model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.APIKey)

	resp, err := o.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("OpenAI 兼容 API 返回 %s：%s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("OpenAI 兼容 API 未返回 choices")
	}
	return out.Choices[0].Message.Content, nil
}
