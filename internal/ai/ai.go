// Package ai 封装对话式 LLM 提供方，供 inhomo 生成自然语言隐私报告（inhomo report）。
// 只把**聚合**（类别计数、归属公司、节点/地区）喂给模型，绝不发原始访问域名（见 ADR-0012）。
package ai

import (
	"context"
	"fmt"
)

// Provider 是一个可从提示词生成文本的 LLM 提供方。抽象成接口以便注入 stub 测试
// （真实 Anthropic 或本地 stub），也为换/加提供方留位。
type Provider interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// New 按名字建 Provider：
//   - "" / "anthropic"：Anthropic Messages API（x-api-key，响应 content[].text）。
//   - "openai"：OpenAI 兼容 Chat Completions（Bearer，响应 choices[].message.content）——
//     可对接 DeepSeek / OpenAI / Groq / 本地 Ollama 等（各自基址经 baseURL 传入）。
//
// baseURL 为空则各 provider 用自己的官方默认。
func New(provider, apiKey, model, baseURL string) (Provider, error) {
	switch provider {
	case "", "anthropic":
		a := NewAnthropic(apiKey, model)
		if baseURL != "" {
			a.BaseURL = baseURL
		}
		return a, nil
	case "openai":
		o := NewOpenAI(apiKey, model)
		if baseURL != "" {
			o.BaseURL = baseURL
		}
		return o, nil
	default:
		return nil, fmt.Errorf("不支持的 AI provider %q（可选：anthropic / openai）", provider)
	}
}
