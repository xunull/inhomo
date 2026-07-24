// Package ai 封装对话式 LLM 提供方，供 inhomo 生成自然语言隐私报告（inhomo report）。
// 只把**聚合**（类别计数、归属公司、节点/地区）喂给模型，绝不发原始访问域名（见 ADR-0012）。
package ai

import "context"

// Provider 是一个可从提示词生成文本的 LLM 提供方。抽象成接口以便注入 stub 测试
// （真实 Anthropic 或本地 stub），也为将来换/加提供方留位。
type Provider interface {
	Generate(ctx context.Context, prompt string) (string, error)
}
