package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xunull/inhomo/internal/store"
)

// TestBuildReportPrompt：提示词含各聚合数字与归属/节点/地区标签；且**不含任何原始访问域名**（隐私）。
func TestBuildReportPrompt(t *testing.T) {
	d := reportData{
		Since:    "7d",
		Trackers: TrackerBreakdown{Total: 100, Tracker: 40, Owners: []OwnerCount{{Owner: "Google", Count: 30}, {Owner: "", Count: 10}}},
		Nodes:    []store.AggRow{{Key: "🇺🇸美国HY2-08", Count: 25}, {Key: "DIRECT", Count: 60}},
		Regions:  []store.AggRow{{Key: "US", Count: 25}, {Key: "", Count: 5}},
	}
	p := buildReportPrompt(d)

	for _, want := range []string{"近 7d", "连接总数：100", "命中已知追踪器：40", "Google：30", "（未知归属）：10", "🇺🇸美国HY2-08：25", "US：25", "unknown：5"} {
		if !strings.Contains(p, want) {
			t.Errorf("提示词应含 %q，实际：\n%s", want, p)
		}
	}
	// 隐私：素材里没有任何 host 字段，提示词自然不含原始访问域名。抽查一个绝不该出现的域名形态。
	if strings.Contains(p, "http") || strings.Contains(p, ".com") {
		t.Errorf("提示词疑似含原始域名/URL，实际：\n%s", p)
	}
}

// TestFetchReportData：对 stub serve，验证从 /api/trackers 与 /api/aggregate 取回聚合。
func TestFetchReportData(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/trackers"):
			_, _ = w.Write([]byte(`{"total":100,"tracker":40,"owners":[{"owner":"Google","count":30}]}`))
		case r.URL.Query().Get("by") == "node":
			_, _ = w.Write([]byte(`[{"key":"🇺🇸US","count":25}]`))
		case r.URL.Query().Get("by") == "region":
			_, _ = w.Write([]byte(`[{"key":"US","count":25}]`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	d, err := fetchReportData(context.Background(), addr, "7d")
	if err != nil {
		t.Fatal(err)
	}
	if d.Trackers.Total != 100 || d.Trackers.Tracker != 40 || len(d.Trackers.Owners) != 1 {
		t.Errorf("trackers 取回不符：%+v", d.Trackers)
	}
	if len(d.Nodes) != 1 || d.Nodes[0].Key != "🇺🇸US" {
		t.Errorf("nodes 取回不符：%+v", d.Nodes)
	}
	if len(d.Regions) != 1 || d.Regions[0].Key != "US" {
		t.Errorf("regions 取回不符：%+v", d.Regions)
	}
}

// TestFetchReportData_serveDown：serve 不可达 → 报错（供 runReport 转成友好提示）。
func TestFetchReportData_serveDown(t *testing.T) {
	if _, err := fetchReportData(context.Background(), "127.0.0.1:1", "7d"); err == nil {
		t.Error("serve 不可达应报错")
	}
}
