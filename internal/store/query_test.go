package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_summary(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "s.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 空库：全 0、时间 nil、不报错。
	sm, err := s.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if sm.Total != 0 || sm.Earliest != nil || sm.Latest != nil {
		t.Fatalf("空库应 0/nil，得 %+v", sm)
	}

	now := time.Now()
	evs := []Event{
		{TS: now, Process: "codex", Network: "TCP", Host: "chatgpt.com", Port: 443, Node: "🇺🇸US", Region: "US"},
		{TS: now, Process: "codex", Network: "TCP", Host: "chatgpt.com", Port: 443, Node: "🇺🇸US", Region: "US"}, // 同 host/process/node
		{TS: now, Process: "", Network: "TCP", Host: "plain.cn", Port: 80, Node: "DIRECT", Region: "unknown"},
		{TS: now, Process: "chrome", Network: "TCP", Host: "example.com", Port: 80, Node: "🇭🇰HK", Region: "HK"},
		{TS: now, Process: "app1", Network: "TCP", Host: "blocked.com", Port: 443, Node: "REJECT", Region: "unknown"}, // REJECT 不算 direct 也不算 proxied
		{TS: now, Process: "", Network: "UDP", Host: "internal.svc", Port: 53, Node: "", Region: "unknown"},           // 空节点同理
	}
	for _, e := range evs {
		if err := s.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	sm, err = s.Summary()
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name      string
		got, want int64
	}{
		{"Total", sm.Total, 6},
		{"Hosts", sm.Hosts, 5},
		{"Processes", sm.Processes, 3}, // codex / chrome / app1（空不计）
		{"Nodes", sm.Nodes, 5},         // US / DIRECT / HK / REJECT / 空
		{"Direct", sm.Direct, 1},       // 仅 node=DIRECT
		{"Proxied", sm.Proxied, 3},     // US×2 + HK（REJECT、空 都排除）
		{"HTTP", sm.HTTP, 2},           // port 80 ×2
		{"HTTPS", sm.HTTPS, 3},         // port 443 ×3
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s=%d，期望 %d", c.name, c.got, c.want)
		}
	}
	if sm.Earliest == nil || sm.Latest == nil {
		t.Error("有数据时 Earliest/Latest 不应为 nil")
	}
}

func TestStore_aggregate(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "a.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	add := func(host string, port int, ago time.Duration) {
		if err := s.Add(Event{TS: now.Add(-ago), Host: host, Port: port, Node: "N", Process: "p", Network: "TCP", Region: "US"}); err != nil {
			t.Fatal(err)
		}
	}
	add("a.com", 443, 0)
	add("a.com", 443, 0)
	add("a.com", 80, 0)
	add("b.com", 443, 0)
	add("b.com", 443, 0)
	add("c.com", 443, 2*time.Hour) // 2 小时前
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	// by=host：a.com(3) > b.com(2) > c.com(1)
	rows, err := s.Aggregate("host", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Key != "a.com" || rows[0].Count != 3 {
		t.Fatalf("host 聚合错：%+v", rows)
	}
	// limit=1
	if rows, _ := s.Aggregate("host", 0, 1); len(rows) != 1 || rows[0].Key != "a.com" {
		t.Fatalf("limit=1 错：%+v", rows)
	}
	// by=port：443(5) > 80(1)
	if rows, _ := s.Aggregate("port", 0, 10); rows[0].Key != "443" || rows[0].Count != 5 {
		t.Fatalf("port 聚合错：%+v", rows)
	}
	// since=1h：排除 2h 前的 c.com
	rows, _ = s.Aggregate("host", time.Hour, 10)
	for _, r := range rows {
		if r.Key == "c.com" {
			t.Fatalf("since=1h 不应含 c.com：%+v", rows)
		}
	}
	// 坏维度 → ErrBadDimension（防注入）
	if _, err := s.Aggregate("evil; DROP TABLE", 0, 10); !errors.Is(err, ErrBadDimension) {
		t.Fatalf("坏维度应 ErrBadDimension，得 %v", err)
	}
}

func TestStore_timeseries(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ts.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 空库 → 空序列、不报错。
	if pts, err := s.TimeSeries(time.Hour, time.Minute); err != nil || len(pts) != 0 {
		t.Fatalf("空库应返回空序列，得 %+v（err %v）", pts, err)
	}

	base := time.Now()
	add := func(ago time.Duration) {
		if err := s.Add(Event{TS: base.Add(-ago), Host: "h", Port: 443, Node: "N", Process: "p", Network: "TCP", Region: "US"}); err != nil {
			t.Fatal(err)
		}
	}
	add(0)
	add(30 * time.Second) // 与 now 同桶（近 1 分钟）
	add(20 * time.Minute) // 20 分钟前 → 另一个桶
	add(90 * time.Minute) // 90 分钟前 → 在 1h 窗外
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	pts, err := s.TimeSeries(time.Hour, time.Minute) // 近 1h，1m 桶
	if err != nil {
		t.Fatal(err)
	}
	// 窗口内总数应为 3（90min 前那条被排除）
	var total int64
	for _, p := range pts {
		total += p.Count
	}
	if total != 3 {
		t.Fatalf("窗口内总数=%d，期望 3（应排除 90min 前）", total)
	}
	// 至少 2 个桶（20min 前那条独立成桶）
	if len(pts) < 2 {
		t.Fatalf("应至少 2 个时间桶，得 %d：%+v", len(pts), pts)
	}
	// 时间升序
	for i := 1; i < len(pts); i++ {
		if !pts[i].TS.After(pts[i-1].TS) {
			t.Fatalf("时间应升序：%+v", pts)
		}
	}
	// 极小窗不报错
	if _, err := s.TimeSeries(time.Second, time.Second); err != nil {
		t.Fatalf("极小窗应无错：%v", err)
	}
}
