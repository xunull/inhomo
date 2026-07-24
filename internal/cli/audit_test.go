package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/xunull/inhomo/internal/detect"
	"github.com/xunull/inhomo/internal/tracker"
)

// TestTrackerNote：命中已知追踪器 → 带归属标注；无归属名 → 省略公司；未命中 / nil 归类器 → 无标注、不 panic。
func TestTrackerNote(t *testing.T) {
	c, err := tracker.Parse([]byte(`{"google-analytics.com":{"displayName":"Google"},"noowner.com":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ host, want string }{
		{"www.google-analytics.com", "  [已知追踪器 · Google]"}, // 子域名归到 eTLD+1
		{"noowner.com", "  [已知追踪器]"},                       // 命中但无归属名
		{"example.com", ""},                                // 未命中
		{"1.2.3.4", ""},                                    // IP 无 eTLD+1
	}
	for _, tc := range cases {
		if got := trackerNote(c, tc.host); got != tc.want {
			t.Errorf("trackerNote(%q)=%q，期望 %q", tc.host, got, tc.want)
		}
	}
	if got := trackerNote(nil, "google-analytics.com"); got != "" {
		t.Errorf("nil 归类器应无标注、不 panic，得 %q", got)
	}
}

// TestFormatLeakLine_annotation：命中追踪器时泄露行含归属标注。
func TestFormatLeakLine_annotation(t *testing.T) {
	c, _ := tracker.Parse([]byte(`{"scorecardresearch.com":{"displayName":"comScore"}}`))
	leak := detect.LeakEvent{Host: "sb.scorecardresearch.com", Port: 80, Node: "🇺🇸US", Region: "US", Rule: "GEOIP"}
	line := formatLeakLine(time.Now(), leak, 0, 5*time.Minute, c)
	if !strings.Contains(line, "[已知追踪器 · comScore]") {
		t.Errorf("命中追踪器的泄露行应含归属标注，得 %q", line)
	}

	// 未命中 → 无标注
	plain := detect.LeakEvent{Host: "example.com", Port: 80, Node: "🇺🇸US", Region: "US", Rule: "GEOIP"}
	if strings.Contains(formatLeakLine(time.Now(), plain, 0, 5*time.Minute, c), "已知追踪器") {
		t.Error("未命中追踪器的泄露行不应含标注")
	}
}
