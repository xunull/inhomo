package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOpenAIGenerate：对 stub OpenAI 兼容 API，验证 Bearer 头、/chat/completions 路径、请求体、
// 及从 choices[0].message.content 取回内容。
func TestOpenAIGenerate(t *testing.T) {
	var gotAuth, gotPath, gotModel, gotPrompt string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role, Content string
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		gotModel = req.Model
		if len(req.Messages) > 0 {
			gotPrompt = req.Messages[0].Content
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"隐私周报正文"}}]}`))
	}))
	defer ts.Close()

	o := NewOpenAI("sk-deepseek", "deepseek-chat")
	o.BaseURL = ts.URL // DeepSeek 式：基址不含 /v1，路径 /chat/completions 由 provider 追加
	out, err := o.Generate(context.Background(), "总结一下")
	if err != nil {
		t.Fatal(err)
	}
	if out != "隐私周报正文" {
		t.Errorf("输出=%q，期望 隐私周报正文", out)
	}
	if gotAuth != "Bearer sk-deepseek" || gotPath != "/chat/completions" || gotModel != "deepseek-chat" || gotPrompt != "总结一下" {
		t.Errorf("请求不符：auth=%q path=%q model=%q prompt=%q", gotAuth, gotPath, gotModel, gotPrompt)
	}
}

// TestOpenAIGenerate_errAndEmpty：非 200 带响应体报错；choices 为空也报错（不 panic）。
func TestOpenAIGenerate_errAndEmpty(t *testing.T) {
	t.Run("非 200 → 带响应体", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Authentication Fails"}}`))
		}))
		defer ts.Close()
		o := NewOpenAI("bad", "deepseek-chat")
		o.BaseURL = ts.URL
		if _, err := o.Generate(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "Authentication Fails") {
			t.Errorf("非 200 应带响应体报错，得 %v", err)
		}
	})

	t.Run("空 choices → 报错", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[]}`))
		}))
		defer ts.Close()
		o := NewOpenAI("k", "m")
		o.BaseURL = ts.URL
		if _, err := o.Generate(context.Background(), "x"); err == nil {
			t.Error("空 choices 应报错")
		}
	})
}

// TestNew：工厂按名字建对的 provider，未知名报错。
func TestNew(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
		is      func(Provider) bool
	}{
		{"", false, func(p Provider) bool { _, ok := p.(*Anthropic); return ok }},
		{"anthropic", false, func(p Provider) bool { _, ok := p.(*Anthropic); return ok }},
		{"openai", false, func(p Provider) bool { _, ok := p.(*OpenAI); return ok }},
		{"grok", true, nil},
	}
	for _, c := range cases {
		p, err := New(c.name, "k", "m", "https://x")
		if c.wantErr {
			if err == nil {
				t.Errorf("provider %q 应报错", c.name)
			}
			continue
		}
		if err != nil || !c.is(p) {
			t.Errorf("provider %q 建错：err=%v type=%T", c.name, err, p)
		}
	}
}

// TestNew_baseURL：传入 baseURL 应写进对应 provider。
func TestNew_baseURL(t *testing.T) {
	p, _ := New("openai", "k", "m", "https://api.deepseek.com")
	if o, ok := p.(*OpenAI); !ok || o.BaseURL != "https://api.deepseek.com" {
		t.Errorf("baseURL 未写入 OpenAI provider：%+v", p)
	}
	// 空 baseURL 不覆盖默认（各 provider 内部按空判定用官方默认）。
	p2, _ := New("openai", "k", "m", "")
	if o, ok := p2.(*OpenAI); !ok || o.BaseURL != "" {
		t.Errorf("空 baseURL 不应写入，得 %+v", p2)
	}
}
