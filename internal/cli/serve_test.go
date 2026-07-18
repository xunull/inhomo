package cli

import "testing"

func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8464", true},
		{"localhost:8464", true},
		{"[::1]:8464", true},
		{"0.0.0.0:8464", false},
		{"192.168.1.10:8464", false},
		{"127.0.0.1", true}, // 无端口也判回环
	}
	for _, c := range cases {
		if got := isLoopbackAddr(c.addr); got != c.want {
			t.Errorf("isLoopbackAddr(%q)=%v，期望 %v", c.addr, got, c.want)
		}
	}
}
