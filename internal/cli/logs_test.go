package cli

import (
	"testing"

	"github.com/xunull/inhomo/internal/logstream"
)

// TestFormatLogLine 断言原样日志行 = 级别标记 + payload；warning/error 归一化为短标记。
func TestFormatLogLine(t *testing.T) {
	cases := []struct {
		msg  logstream.LogMessage
		want string
	}{
		{logstream.LogMessage{Type: "info", Payload: "[TCP] a --> b:80 using X"}, "[info] [TCP] a --> b:80 using X"},
		{logstream.LogMessage{Type: "warning", Payload: "[TCP] dial ... error"}, "[warn] [TCP] dial ... error"},
		{logstream.LogMessage{Type: "error", Payload: "boom"}, "[err] boom"},
		{logstream.LogMessage{Type: "", Payload: "x"}, "[?] x"},
	}
	for _, c := range cases {
		if got := formatLogLine(c.msg); got != c.want {
			t.Errorf("formatLogLine(%+v)=%q，期望 %q", c.msg, got, c.want)
		}
	}
}
