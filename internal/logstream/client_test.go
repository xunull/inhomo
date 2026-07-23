package logstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestUnixSocketPath 覆盖 unix:// 形式识别与 socket 路径提取。
func TestUnixSocketPath(t *testing.T) {
	cases := []struct {
		in       string
		wantPath string
		wantOK   bool
	}{
		{"unix:///tmp/verge/verge-mihomo.sock", "/tmp/verge/verge-mihomo.sock", true},
		{"unix:/tmp/x.sock", "/tmp/x.sock", true},
		{"127.0.0.1:9090", "", false},
		{"http://127.0.0.1:9090", "", false},
	}
	for _, c := range cases {
		p, ok := unixSocketPath(c.in)
		if ok != c.wantOK || p != c.wantPath {
			t.Errorf("unixSocketPath(%q)=(%q,%v)，期望 (%q,%v)", c.in, p, ok, c.wantPath, c.wantOK)
		}
	}
}

// TestNew_baseURL 覆盖 TCP 与 Unix socket 两种控制器归一化后的 BaseURL。
func TestNew_baseURL(t *testing.T) {
	cases := []struct{ ctrl, wantBase string }{
		{"127.0.0.1:9090", "http://127.0.0.1:9090"},
		{"http://127.0.0.1:9097/", "http://127.0.0.1:9097"},
		{"unix:///tmp/x.sock", "http://localhost"},
	}
	for _, c := range cases {
		if got := New(c.ctrl, "").BaseURL; got != c.wantBase {
			t.Errorf("New(%q).BaseURL=%q，期望 %q", c.ctrl, got, c.wantBase)
		}
	}
}

// TestAlive 覆盖 /version 探活：200 判活、非 200/不可达判非活、且探活请求带上 secret。
// 这是零参数自动发现「探活挑真活的那个」所依赖的不纯原语。
func TestAlive(t *testing.T) {
	t.Run("200 → 活", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/version" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"version":"test"}`))
		}))
		defer ts.Close()
		if !New(strings.TrimPrefix(ts.URL, "http://"), "").Alive(context.Background()) {
			t.Error("/version 返回 200 应判活")
		}
	})

	t.Run("401 → 非活", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer ts.Close()
		if New(strings.TrimPrefix(ts.URL, "http://"), "wrong").Alive(context.Background()) {
			t.Error("/version 返回 401 应判非活")
		}
	})

	t.Run("不可达 → 非活", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		if New("127.0.0.1:1", "").Alive(ctx) { // 端口 1 基本不可达
			t.Error("不可达控制器应判非活")
		}
	})

	t.Run("探活带上 secret", func(t *testing.T) {
		got := make(chan string, 1)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got <- r.Header.Get("Authorization")
			_, _ = w.Write([]byte("{}"))
		}))
		defer ts.Close()
		New(strings.TrimPrefix(ts.URL, "http://"), "s3cr3t").Alive(context.Background())
		if h := <-got; h != "Bearer s3cr3t" {
			t.Errorf("探活 Authorization=%q，期望 Bearer s3cr3t", h)
		}
	})
}
