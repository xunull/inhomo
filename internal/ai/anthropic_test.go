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

// TestAnthropicGenerate：对 stub Messages API，验证请求头/体正确、文本内容块被拼接返回。
func TestAnthropicGenerate(t *testing.T) {
	var gotKey, gotVer, gotModel, gotPrompt string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotVer = r.Header.Get("anthropic-version")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model    string                     `json:"model"`
			Messages []struct{ Content string } `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		gotModel = req.Model
		if len(req.Messages) > 0 {
			gotPrompt = req.Messages[0].Content
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"隐私"},{"type":"text","text":"周报"}]}`))
	}))
	defer ts.Close()

	a := NewAnthropic("sk-test", "claude-sonnet-5")
	a.BaseURL = ts.URL
	out, err := a.Generate(context.Background(), "总结一下")
	if err != nil {
		t.Fatal(err)
	}
	if out != "隐私周报" { // 多个 text 块拼接
		t.Errorf("输出=%q，期望 隐私周报", out)
	}
	if gotKey != "sk-test" || gotVer != anthropicVersion || gotModel != "claude-sonnet-5" || gotPrompt != "总结一下" {
		t.Errorf("请求头/体不符：key=%q ver=%q model=%q prompt=%q", gotKey, gotVer, gotModel, gotPrompt)
	}
}

// TestAnthropicGenerate_filtersNonText：只拼接 type=="text" 的内容块，忽略 thinking 等其它块。
func TestAnthropicGenerate_filtersNonText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"thinking","text":"忽略我"},{"type":"text","text":"要的正文"}]}`))
	}))
	defer ts.Close()
	a := NewAnthropic("k", "m")
	a.BaseURL = ts.URL
	out, err := a.Generate(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if out != "要的正文" {
		t.Errorf("应只取 text 块，得 %q", out)
	}
}

// TestAnthropicGenerate_errStatus：非 200 → 报错且带响应体。
func TestAnthropicGenerate_errStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
	}))
	defer ts.Close()
	a := NewAnthropic("bad", "claude-sonnet-5")
	a.BaseURL = ts.URL
	_, err := a.Generate(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "invalid key") {
		t.Errorf("非 200 应报错并带响应体，得 %v", err)
	}
}
