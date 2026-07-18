package cli

import (
	"path/filepath"
	"testing"
)

// TestResolveDBPath 覆盖库路径解析：默认、~ 展开、绝对/相对原样。
func TestResolveDBPath(t *testing.T) {
	home := filepath.Join("/home", "u")
	cases := []struct{ in, want string }{
		{"", filepath.Join(home, ".inhomo", "connections.duckdb")},
		{"~/x.duckdb", filepath.Join(home, "x.duckdb")},
		{"~/data/y.duckdb", filepath.Join(home, "data", "y.duckdb")},
		{"/abs/z.duckdb", "/abs/z.duckdb"},
		{"rel.duckdb", "rel.duckdb"},
	}
	for _, c := range cases {
		if got := resolveDBPath(c.in, home); got != c.want {
			t.Errorf("resolveDBPath(%q)=%q，期望 %q", c.in, got, c.want)
		}
	}
}
