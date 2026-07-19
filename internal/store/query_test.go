package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// seed 建一个临时库并写入给定事件（已 Flush，可查）。
func seed(t *testing.T, evs []Event) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "s.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, e := range evs {
		if err := s.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStore_summary(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "s.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 空库：全 0、时间 nil、不报错。
	sm, err := s.Summary(Filter{})
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

	sm, err = s.Summary(Filter{})
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
	rows, err := s.Aggregate("host", Filter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Key != "a.com" || rows[0].Count != 3 {
		t.Fatalf("host 聚合错：%+v", rows)
	}
	// limit=1
	if rows, _ := s.Aggregate("host", Filter{}, 1); len(rows) != 1 || rows[0].Key != "a.com" {
		t.Fatalf("limit=1 错：%+v", rows)
	}
	// by=port：443(5) > 80(1)
	if rows, _ := s.Aggregate("port", Filter{}, 10); rows[0].Key != "443" || rows[0].Count != 5 {
		t.Fatalf("port 聚合错：%+v", rows)
	}
	// since=1h：排除 2h 前的 c.com
	rows, _ = s.Aggregate("host", Filter{Since: time.Hour}, 10)
	for _, r := range rows {
		if r.Key == "c.com" {
			t.Fatalf("since=1h 不应含 c.com：%+v", rows)
		}
	}
	// 坏维度 → ErrBadDimension（防注入）
	if _, err := s.Aggregate("evil; DROP TABLE", Filter{}, 10); !errors.Is(err, ErrBadDimension) {
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
	if pts, err := s.TimeSeries(Filter{Since: time.Hour}, time.Minute); err != nil || len(pts) != 0 {
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

	pts, err := s.TimeSeries(Filter{Since: time.Hour}, time.Minute) // 近 1h，1m 桶
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
	if _, err := s.TimeSeries(Filter{Since: time.Second}, time.Second); err != nil {
		t.Fatalf("极小窗应无错：%v", err)
	}
}

// TestStore_filter 覆盖过滤切片：精确列、route 谓词、组合，作用于 Summary 与 Aggregate。
func TestStore_filter(t *testing.T) {
	now := time.Now()
	p80, p443 := 80, 443
	s := seed(t, []Event{
		{TS: now, Process: "gh", Network: "TCP", Host: "api.github.com", Port: 443, Node: "🇺🇸US", Region: "US"},
		{TS: now, Process: "gh", Network: "TCP", Host: "api.github.com", Port: 80, Node: "🇺🇸US", Region: "US"},
		{TS: now, Process: "curl", Network: "TCP", Host: "api.github.com", Port: 80, Node: "DIRECT", Region: "unknown"},
		{TS: now, Process: "curl", Network: "TCP", Host: "example.com", Port: 80, Node: "🇭🇰HK", Region: "HK"},
		{TS: now, Process: "app", Network: "TCP", Host: "blocked.com", Port: 443, Node: "REJECT", Region: "unknown"},
	})

	// host=api.github.com → 3 条
	if sm, _ := s.Summary(Filter{Host: "api.github.com"}); sm.Total != 3 {
		t.Errorf("host 过滤 Total=%d，期望 3", sm.Total)
	}
	// port=80 → 3 条
	if sm, _ := s.Summary(Filter{Port: &p80}); sm.Total != 3 {
		t.Errorf("port=80 Total=%d，期望 3", sm.Total)
	}
	// route=direct → 仅 node=DIRECT 的 1 条
	if sm, _ := s.Summary(Filter{Route: "direct"}); sm.Total != 1 {
		t.Errorf("route=direct Total=%d，期望 1", sm.Total)
	}
	// route=proxied → US×2 + HK（DIRECT/REJECT 排除）= 3
	if sm, _ := s.Summary(Filter{Route: "proxied"}); sm.Total != 3 {
		t.Errorf("route=proxied Total=%d，期望 3", sm.Total)
	}
	// 组合：host=api.github.com 且 port=443 → 1 条
	if sm, _ := s.Summary(Filter{Host: "api.github.com", Port: &p443}); sm.Total != 1 {
		t.Errorf("host+port 组合 Total=%d，期望 1", sm.Total)
	}
	// 组合：port=80 且 route=proxied → 只有 api.github.com(US,80) 与 example.com(HK,80) = 2
	if sm, _ := s.Summary(Filter{Port: &p80, Route: "proxied"}); sm.Total != 2 {
		t.Errorf("port80+proxied Total=%d，期望 2", sm.Total)
	}

	// Aggregate 也受过滤：host=api.github.com 下按 process 分 → gh(2) > curl(1)
	rows, err := s.Aggregate("process", Filter{Host: "api.github.com"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Key != "gh" || rows[0].Count != 2 {
		t.Fatalf("过滤后 process 聚合错：%+v", rows)
	}
}

// TestStore_connections 覆盖原始明细：过滤、总数、时间倒序、分页。
func TestStore_connections(t *testing.T) {
	base := time.Now()
	// 5 条 api.github.com（时间递减），2 条别的 host。
	evs := []Event{}
	for i := range 5 {
		evs = append(evs, Event{TS: base.Add(-time.Duration(i) * time.Minute), Process: "gh", Network: "TCP",
			Host: "api.github.com", Port: 443, Rule: "R", Node: "🇺🇸US", Region: "US"})
	}
	evs = append(evs,
		Event{TS: base, Host: "other.com", Port: 80, Node: "DIRECT", Network: "TCP"},
		Event{TS: base, Host: "other2.com", Port: 80, Node: "DIRECT", Network: "TCP"},
	)
	s := seed(t, evs)

	// 空库切片：total=0、rows 为空、不报错。
	if pg, err := s.Connections(Filter{Host: "none"}, 0, 50); err != nil || pg.Total != 0 || len(pg.Rows) != 0 {
		t.Fatalf("空切片应 total=0 空 rows，得 %+v（err %v）", pg, err)
	}

	// host 过滤：total=5，默认页返回全部 5 行，时间倒序。
	pg, err := s.Connections(Filter{Host: "api.github.com"}, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if pg.Total != 5 || len(pg.Rows) != 5 {
		t.Fatalf("host 过滤应 total=5、5 行，得 total=%d rows=%d", pg.Total, len(pg.Rows))
	}
	for i := 1; i < len(pg.Rows); i++ {
		if pg.Rows[i].TS.After(pg.Rows[i-1].TS) {
			t.Fatalf("明细应按时间倒序：%+v", pg.Rows)
		}
	}
	if pg.Rows[0].Port != 443 || pg.Rows[0].Process != "gh" {
		t.Errorf("行字段错：%+v", pg.Rows[0])
	}

	// 分页：limit=2 offset=0 → 2 行但 total 仍 5。
	pg1, _ := s.Connections(Filter{Host: "api.github.com"}, 0, 2)
	if pg1.Total != 5 || len(pg1.Rows) != 2 {
		t.Fatalf("分页第一页应 total=5、2 行，得 total=%d rows=%d", pg1.Total, len(pg1.Rows))
	}
	// offset=4 → 剩 1 行。
	pg2, _ := s.Connections(Filter{Host: "api.github.com"}, 4, 2)
	if len(pg2.Rows) != 1 {
		t.Fatalf("offset=4 应剩 1 行，得 %d", len(pg2.Rows))
	}
	// 第二页第一行应比第一页第一行更早（时间倒序 + offset 生效）。
	if !pg2.Rows[0].TS.Before(pg1.Rows[0].TS) {
		t.Errorf("offset 未生效或顺序错")
	}

	// 全集：total=7。
	if pg, _ := s.Connections(Filter{}, 0, 50); pg.Total != 7 {
		t.Errorf("全集 total=%d，期望 7", pg.Total)
	}
}
