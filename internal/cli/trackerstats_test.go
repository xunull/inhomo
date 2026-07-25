package cli

import (
	"testing"

	"github.com/xunull/inhomo/internal/store"
	"github.com/xunull/inhomo/internal/tracker"
)

// TestComputeTrackerBreakdown：按 host 连接数归并成追踪器占比 + 归属 top-N。
func TestComputeTrackerBreakdown(t *testing.T) {
	c, err := tracker.Parse([]byte(`{
		"google-analytics.com": {"displayName": "Google"},
		"doubleclick.net": {"displayName": "Google"},
		"scorecardresearch.com": {"displayName": "comScore"},
		"noowner.com": {}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	hosts := []store.AggRow{
		{Key: "www.google-analytics.com", Count: 10}, // Google
		{Key: "doubleclick.net", Count: 5},           // Google（同归属累加）
		{Key: "sb.scorecardresearch.com", Count: 4},  // comScore
		{Key: "noowner.com", Count: 3},               // 命中但无归属名 → 空归属桶（标签交前端）
		{Key: "example.com", Count: 8},               // 未命中 → 不计入 tracker
		{Key: "1.2.3.4", Count: 2},                   // IP → 未命中
	}
	got := computeTrackerBreakdown(hosts, c, 10)

	if got.Total != 32 {
		t.Errorf("Total=%d，期望 32", got.Total)
	}
	if got.Tracker != 22 { // 10+5+4+3
		t.Errorf("Tracker=%d，期望 22", got.Tracker)
	}
	if !got.Loaded {
		t.Error("有数据的归类器应 Loaded=true")
	}
	want := []OwnerCount{
		{Owner: "Google", Count: 15},
		{Owner: "comScore", Count: 4},
		{Owner: "", Count: 3}, // 无归属名桶（后端出空串，标签交前端）
	}
	if len(got.Owners) != len(want) {
		t.Fatalf("Owners=%+v，期望 %+v", got.Owners, want)
	}
	for i := range want {
		if got.Owners[i] != want[i] {
			t.Errorf("Owners[%d]=%+v，期望 %+v", i, got.Owners[i], want[i])
		}
	}
}

// TestComputeTrackerBreakdown_topN 与空/ nil 归类器边界。
func TestComputeTrackerBreakdown_edges(t *testing.T) {
	c, _ := tracker.Parse([]byte(`{"a.com":{"displayName":"A"},"b.com":{"displayName":"B"},"c.com":{"displayName":"C"}}`))
	hosts := []store.AggRow{{Key: "a.com", Count: 3}, {Key: "b.com", Count: 2}, {Key: "c.com", Count: 1}}

	t.Run("topN 截断", func(t *testing.T) {
		got := computeTrackerBreakdown(hosts, c, 2)
		if len(got.Owners) != 2 || got.Owners[0].Owner != "A" || got.Owners[1].Owner != "B" {
			t.Errorf("topN=2 应留 A,B，得 %+v", got.Owners)
		}
		if got.Tracker != 6 { // 截断不影响总计
			t.Errorf("Tracker=%d，期望 6（截断只影响 Owners 列表长度）", got.Tracker)
		}
	})

	t.Run("同分按归属名升序（tie-break 确定性）", func(t *testing.T) {
		c2, _ := tracker.Parse([]byte(`{"z.com":{"displayName":"Zeta"},"a.com":{"displayName":"Alpha"}}`))
		// 两家归属同为 5 条，应按名字升序 Alpha 在前、Zeta 在后。
		got := computeTrackerBreakdown([]store.AggRow{{Key: "z.com", Count: 5}, {Key: "a.com", Count: 5}}, c2, 10)
		if len(got.Owners) != 2 || got.Owners[0].Owner != "Alpha" || got.Owners[1].Owner != "Zeta" {
			t.Errorf("同分应按名升序 [Alpha,Zeta]，得 %+v", got.Owners)
		}
	})

	t.Run("nil 归类器 → 全非追踪器", func(t *testing.T) {
		got := computeTrackerBreakdown(hosts, nil, 10)
		if got.Tracker != 0 || len(got.Owners) != 0 {
			t.Errorf("nil 归类器应零追踪器，得 %+v", got)
		}
		if got.Total != 6 {
			t.Errorf("Total 仍应为 6，得 %d", got.Total)
		}
		if got.Loaded {
			t.Error("nil 归类器应 Loaded=false（前端据此提示 tracker update）")
		}
	})
}
