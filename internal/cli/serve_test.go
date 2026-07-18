package cli

import (
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},
		{"24h", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"90m", 90 * time.Minute, false},
		{"bad", 0, true},
		{"xd", 0, true},
		{"-5d", 0, true}, // 负天数应报错，而非静默全量
	}
	for _, c := range cases {
		got, err := parseSince(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseSince(%q) err=%v，wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.want {
			t.Errorf("parseSince(%q)=%v，期望 %v", c.in, got, c.want)
		}
	}
}

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
