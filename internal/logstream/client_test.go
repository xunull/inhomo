package logstream

import "testing"

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
