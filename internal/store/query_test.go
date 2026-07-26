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

// seedTraffic 建临时库并写入流量记录（AddTraffic 走直接 INSERT，无需 Flush 即可查）。
func seedTraffic(t *testing.T, recs []TrafficRecord) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "tr.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, r := range recs {
		if err := s.AddTraffic(r); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// TestStore_traffic 覆盖带宽聚合：三种 metric 排序、top-N、切片总量、过滤、时间窗、
// 维度白名单（含 rule 必须被挡——traffic 无此列）与 metric 白名单。
func TestStore_traffic(t *testing.T) {
	now := time.Now()
	mk := func(host, node, region string, up, down int64, ago time.Duration) TrafficRecord {
		return TrafficRecord{
			Start: now.Add(-ago), Process: "p", Network: "tcp",
			Host: host, Port: 443, Node: node, Region: region,
			UpBytes: up, DownBytes: down, DurationMs: 1000,
		}
	}

	// 空库：空 rows（非 nil，JSON 为 []）、总量 0、不报错。
	if ag, err := seedTraffic(t, nil).Traffic("host", "total", Filter{}, 10); err != nil ||
		ag.Rows == nil || len(ag.Rows) != 0 || ag.TotalUp != 0 || ag.TotalDown != 0 {
		t.Fatalf("空库应空 rows/0 总量，得 %+v（err %v）", ag, err)
	}

	s := seedTraffic(t, []TrafficRecord{
		mk("a.com", "🇺🇸US", "US", 100, 1000, 0),
		mk("a.com", "🇺🇸US", "US", 50, 500, 0),                 // a.com 合计 up150 down1500
		mk("b.com", "🇭🇰HK", "HK", 900, 200, 0),                // b.com up900 down200
		mk("c.com", "DIRECT", "unknown", 10, 10, 2*time.Hour), // 2h 前
	})

	// 切片总上/下行（全集）：up=100+50+900+10=1060，down=1000+500+200+10=1710。
	ag, err := s.Traffic("host", "total", Filter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if ag.TotalUp != 1060 || ag.TotalDown != 1710 {
		t.Fatalf("总量错：up=%d down=%d（期望 1060/1710）", ag.TotalUp, ag.TotalDown)
	}
	// metric=total：按 up+down 排序 → a.com(1650) > b.com(1100) > c.com(20)。
	if ag.Rows[0].Key != "a.com" || ag.Rows[0].UpBytes != 150 || ag.Rows[0].DownBytes != 1500 {
		t.Fatalf("total 排序/取值错：%+v", ag.Rows)
	}
	// metric=up：按 up 排序 → b.com(900) 居首。
	if up, _ := s.Traffic("host", "up", Filter{}, 10); up.Rows[0].Key != "b.com" || up.Rows[0].UpBytes != 900 {
		t.Fatalf("up 排序错：%+v", up.Rows)
	}
	// metric=down：按 down 排序 → a.com(1500) 居首。
	if dn, _ := s.Traffic("host", "down", Filter{}, 10); dn.Rows[0].Key != "a.com" || dn.Rows[0].DownBytes != 1500 {
		t.Fatalf("down 排序错：%+v", dn.Rows)
	}
	// top-N：limit=1 → 仅 1 行。
	if one, _ := s.Traffic("host", "total", Filter{}, 1); len(one.Rows) != 1 {
		t.Fatalf("limit=1 应 1 行，得 %d", len(one.Rows))
	}
	// since=1h：rows 与总量都排除 2h 前的 c.com。
	win, _ := s.Traffic("host", "up", Filter{Since: time.Hour}, 10)
	for _, r := range win.Rows {
		if r.Key == "c.com" {
			t.Fatalf("since=1h 不应含 c.com：%+v", win.Rows)
		}
	}
	if win.TotalUp != 1050 { // 1060 - 10
		t.Errorf("since=1h 总上行=%d，期望 1050", win.TotalUp)
	}
	// 过滤 region=HK → 仅 b.com。
	if hk, _ := s.Traffic("host", "total", Filter{Region: "HK"}, 10); len(hk.Rows) != 1 || hk.Rows[0].Key != "b.com" {
		t.Fatalf("region=HK 过滤错：%+v", hk.Rows)
	}
	// by=node 维度可用（走白名单）。
	if bn, _ := s.Traffic("node", "total", Filter{}, 10); len(bn.Rows) == 0 {
		t.Fatal("by=node 不该空")
	}
	// 坏维度 → ErrBadDimension；rule 必须被挡（traffic 无此列，否则 500）。
	if _, err := s.Traffic("rule", "total", Filter{}, 10); !errors.Is(err, ErrBadDimension) {
		t.Fatalf("by=rule 应 ErrBadDimension，得 %v", err)
	}
	if _, err := s.Traffic("evil; DROP", "total", Filter{}, 10); !errors.Is(err, ErrBadDimension) {
		t.Fatalf("坏维度应 ErrBadDimension，得 %v", err)
	}
	// 坏 metric → ErrBadMetric。
	if _, err := s.Traffic("host", "sideways", Filter{}, 10); !errors.Is(err, ErrBadMetric) {
		t.Fatalf("坏 metric 应 ErrBadMetric，得 %v", err)
	}
	// metric 空 → 默认 total（不报错）。
	if _, err := s.Traffic("host", "", Filter{}, 10); err != nil {
		t.Fatalf("空 metric 应默认 total，得 err %v", err)
	}
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

// TestStore_flow 覆盖两层 App→节点 拓扑（连接数度量 count，来自全量连接事件）：
// 基本边、top-N + 其它累加、命名空间不塌陷、过滤。
func TestStore_flow(t *testing.T) {
	// 空库 → 空节点/边，且非 nil（JSON 为 [] 而非 null）。
	if g, err := seed(t, nil).Flow(Filter{}, "count", 10); err != nil || g.Nodes == nil || g.Links == nil ||
		len(g.Nodes) != 0 || len(g.Links) != 0 {
		t.Fatalf("空库应空节点/边（非 nil），得 %+v（err %v）", g, err)
	}

	now := time.Now()
	evs := []Event{}
	addN := func(app, node string, n int) {
		for range n {
			evs = append(evs, Event{TS: now, Process: app, Node: node, Network: "TCP", Host: "h", Port: 443, Region: "R"})
		}
	}
	addN("gh", "US", 5)
	addN("curl", "US", 3)
	addN("codex", "HK", 2)
	addN("wechat", "DIRECT", 4)
	addN("US", "HK", 1) // 进程名恰好叫 "US"：测与 node "US" 不塌陷
	s := seed(t, evs)

	linkVal := func(g FlowGraph, src, dst string) int64 {
		for _, l := range g.Links {
			if l.Source == src && l.Target == dst {
				return l.Value
			}
		}
		return -1
	}
	hasNode := func(g FlowGraph, name string) *FlowNode {
		for i := range g.Nodes {
			if g.Nodes[i].Name == name {
				return &g.Nodes[i]
			}
		}
		return nil
	}

	// limit=10：5 个 App、3 个节点全 fit，无「其它」。
	g, err := s.Flow(Filter{}, "count", 10)
	if err != nil {
		t.Fatal(err)
	}
	if v := linkVal(g, "process:gh", "node:US"); v != 5 {
		t.Errorf("gh→US=%d，期望 5", v)
	}
	if v := linkVal(g, "process:curl", "node:US"); v != 3 {
		t.Errorf("curl→US=%d，期望 3", v)
	}
	if hasNode(g, "process:US") == nil || hasNode(g, "node:US") == nil {
		t.Error("命名空间：process:US 与 node:US 应同时存在、不塌陷")
	}
	if hasNode(g, "process:__other__") != nil {
		t.Error("limit=10 全 fit，不该有「其它」App 桶")
	}

	// limit=2：top-2 App=gh(5)/wechat(4)，top-2 节点=US(8)/DIRECT(4)；其余归其它。
	g2, _ := s.Flow(Filter{}, "count", 2)
	if v := linkVal(g2, "process:gh", "node:US"); v != 5 {
		t.Errorf("gh→US=%d，期望 5", v)
	}
	if v := linkVal(g2, "process:__other__", "node:US"); v != 3 { // curl 折进其它
		t.Errorf("其它→US=%d，期望 3", v)
	}
	if v := linkVal(g2, "process:__other__", "node:__other__"); v != 3 { // codex(HK)2 + US-app(HK)1 累加
		t.Errorf("其它→其它=%d，期望 3（2+1 累加）", v)
	}
	if n := hasNode(g2, "process:__other__"); n == nil || n.Label != "其它" || n.Key != flowOther {
		t.Errorf("其它桶节点异常：%+v", n)
	}

	// 过滤 node=US：只剩指向 US 的边。
	gf, _ := s.Flow(Filter{Node: "US"}, "count", 10)
	if len(gf.Links) == 0 {
		t.Fatal("过滤 node=US 不该为空")
	}
	for _, l := range gf.Links {
		if l.Target != "node:US" {
			t.Errorf("过滤 node=US 后出现非 US 目标：%s", l.Target)
		}
	}
}

// TestStore_flowMetric 覆盖拓扑按字节度量加权（来自抽样的流量记录）：
// 边权=字节合计（而非记录数）、up/down/total 三档不同、top-N 按字节、空 metric 默认 count、非法 metric。
func TestStore_flowMetric(t *testing.T) {
	now := time.Now()
	mk := func(app, node string, up, down int64) TrafficRecord {
		return TrafficRecord{
			Start: now, Process: app, Network: "tcp", Host: "h", Port: 443,
			Node: node, Region: "R", UpBytes: up, DownBytes: down, DurationMs: 1000,
		}
	}
	// gh：仅 1 条但高字节；curl：3 条但低字节 —— 用来证明边权按「字节」而非「记录数」。
	s := seedTraffic(t, []TrafficRecord{
		mk("gh", "US", 1000, 0),
		mk("curl", "US", 1, 1),
		mk("curl", "US", 1, 1),
		mk("curl", "US", 1, 1),
		mk("codex", "HK", 300, 200), // total 500
	})

	linkVal := func(g FlowGraph, src, dst string) int64 {
		for _, l := range g.Links {
			if l.Source == src && l.Target == dst {
				return l.Value
			}
		}
		return -1
	}

	// metric=total：边权=上+下字节合计。gh 仅 1 条却重于 curl 的 3 条 → 证明按字节、非记录数。
	gt, err := s.Flow(Filter{}, "total", 10)
	if err != nil {
		t.Fatal(err)
	}
	if v := linkVal(gt, "process:gh", "node:US"); v != 1000 {
		t.Errorf("total gh→US=%d，期望 1000（字节合计，非 1 条记录）", v)
	}
	if v := linkVal(gt, "process:curl", "node:US"); v != 6 {
		t.Errorf("total curl→US=%d，期望 6（3 条×(1+1)）", v)
	}
	if v := linkVal(gt, "process:codex", "node:HK"); v != 500 {
		t.Errorf("total codex→HK=%d，期望 500", v)
	}

	// metric=up / down：仅方向字节。gh 无下行 → down 边权 0（与 up=1000 分得开）。
	gu, _ := s.Flow(Filter{}, "up", 10)
	if v := linkVal(gu, "process:gh", "node:US"); v != 1000 {
		t.Errorf("up gh→US=%d，期望 1000", v)
	}
	gd, _ := s.Flow(Filter{}, "down", 10)
	if v := linkVal(gd, "process:gh", "node:US"); v != 0 {
		t.Errorf("down gh→US=%d，期望 0（gh 无下行）", v)
	}
	if v := linkVal(gd, "process:curl", "node:US"); v != 3 {
		t.Errorf("down curl→US=%d，期望 3", v)
	}

	// top-N 按字节：limit=1 → top-1 App=gh(1000)、top-1 节点=US(1006)；其余折进「其它」。
	// codex→HK 两端都非 top → 折成 其它→其它=500。
	g1, _ := s.Flow(Filter{}, "total", 1)
	if v := linkVal(g1, "process:gh", "node:US"); v != 1000 {
		t.Errorf("limit=1 total gh→US=%d，期望 1000", v)
	}
	if v := linkVal(g1, "process:__other__", "node:__other__"); v != 500 {
		t.Errorf("limit=1 其它→其它=%d，期望 500（codex→HK 两端折叠）", v)
	}

	// 过滤切片作用在 traffic 表（复用 filter-on-traffic、whereOn("start_ts")）：
	// Node=US → 只剩指向 US 的边，HK 消失。钉住字节路径的 表/时间列 接线正确。
	gus, _ := s.Flow(Filter{Node: "US"}, "total", 10)
	if len(gus.Links) == 0 {
		t.Fatal("过滤 node=US（total）不该为空")
	}
	for _, l := range gus.Links {
		if l.Target != "node:US" {
			t.Errorf("过滤 node=US（total）后出现非 US 目标：%s", l.Target)
		}
	}
	if v := linkVal(gus, "process:gh", "node:US"); v != 1000 {
		t.Errorf("过滤后 total gh→US=%d，期望 1000", v)
	}

	// 空流量表 + 字节度量：空图但非 nil（JSON 为 []），不报错、不误落到 count 路径。
	if ge, err := seedTraffic(t, nil).Flow(Filter{}, "total", 10); err != nil ||
		ge.Nodes == nil || ge.Links == nil || len(ge.Links) != 0 {
		t.Errorf("空流量表（total）应空图非 nil，得 nodes=%v links=%v err=%v", ge.Nodes, ge.Links, err)
	}

	// 空 metric 默认 count：此库无连接事件 → 空图（证明默认走 connections 而非 traffic）。
	if gc, err := s.Flow(Filter{}, "", 10); err != nil || len(gc.Links) != 0 {
		t.Errorf("空 metric 应默认 count；此库无连接事件应空图，得 links=%d err=%v", len(gc.Links), err)
	}

	// 非法 metric → ErrBadMetric（serve 层据此回 400）。
	if _, err := s.Flow(Filter{}, "bogus", 10); !errors.Is(err, ErrBadMetric) {
		t.Errorf("非法 metric 应 ErrBadMetric，得 %v", err)
	}
}

// seedBoth 建临时库并同时写入连接事件（全量，覆盖率分母）与流量记录（抽样，字节账）。
// Exfil 是唯一跨两张表的查询，故需要一个两表都能落的 helper。
func seedBoth(t *testing.T, evs []Event, recs []TrafficRecord) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "ex.duckdb"))
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
	for _, r := range recs {
		if err := s.AddTraffic(r); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// TestStore_exfil 覆盖外发比：按应用通道聚合、比值降序、两种门槛、覆盖率分母取自 connections、
// 下行为 0 不除零、空进程排除、低覆盖率不被隐式过滤。
func TestStore_exfil(t *testing.T) {
	now := time.Now()
	ev := func(proc, host string) Event {
		return Event{TS: now, Process: proc, Network: "tcp", Host: host, Port: 443, Node: "n", Region: "hk"}
	}
	rec := func(proc, host string, up, down int64) TrafficRecord {
		return TrafficRecord{
			Start: now, Process: proc, Network: "tcp", Host: host, Port: 443,
			Node: "n", Region: "hk", UpBytes: up, DownBytes: down, DurationMs: 1000,
		}
	}
	rep := func(n int, r TrafficRecord) []TrafficRecord {
		out := make([]TrafficRecord, n)
		for i := range out {
			out[i] = r
		}
		return out
	}
	repEv := func(n int, e Event) []Event {
		out := make([]Event, n)
		for i := range out {
			out[i] = e
		}
		return out
	}

	// 空库：空切片（非 nil，JSON 为 []）、不报错。
	if rows, err := seedBoth(t, nil, nil).Exfil(Filter{}, 10, 1, 1); err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("空库应返回空切片，得 %+v（err %v）", rows, err)
	}

	var evs []Event
	evs = append(evs, repEv(20, ev("A", "x"))...) // A→x 全量 20 条，只采样到 10 条 → 覆盖率 50%
	evs = append(evs, repEv(10, ev("A", "y"))...)
	evs = append(evs, repEv(2, ev("B", "z"))...)
	// C→q 只有流量记录、无连接事件 → Logged 应为 0（COALESCE 生效）

	var recs []TrafficRecord
	recs = append(recs, rep(10, rec("A", "x", 100, 10))...)   // up 1000 / down 100 → 比值 10
	recs = append(recs, rep(10, rec("A", "y", 100, 1000))...) // up 1000 / down 10000 → 比值 0.1
	recs = append(recs, rep(2, rec("B", "z", 2500, 0))...)    // up 5000 / down 0 → 比值 5000（不除零）
	recs = append(recs, rep(5, rec("", "w", 9999, 1))...)     // 空进程 → 构不成应用通道，须排除
	recs = append(recs, rep(3, rec("C", "q", 300, 100))...)   // 无对应连接事件 → Logged 0

	s := seedBoth(t, evs, recs)

	// 门槛放到最松（显式传 1），验证排序与全部行。
	rows, err := s.Exfil(Filter{}, 10, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Process == "" {
			t.Errorf("空进程不构成应用通道，不应出现：%+v", r)
		}
	}
	want := []struct {
		proc, host string
		ratio      float64
	}{
		{"B", "z", 5000}, // 下行 0 → greatest(down,1)，不 panic 不 Inf
		{"A", "x", 10},
		{"C", "q", 3},
		{"A", "y", 0.1},
	}
	if len(rows) != len(want) {
		t.Fatalf("应有 %d 条通道，得 %d：%+v", len(want), len(rows), rows)
	}
	for i, w := range want {
		if rows[i].Process != w.proc || rows[i].Host != w.host {
			t.Errorf("第 %d 行应为 %s→%s，得 %s→%s（比值须降序）", i, w.proc, w.host, rows[i].Process, rows[i].Host)
		}
		if diff := rows[i].Ratio - w.ratio; diff > 0.001 || diff < -0.001 {
			t.Errorf("%s→%s 外发比应约 %v，得 %v", w.proc, w.host, w.ratio, rows[i].Ratio)
		}
	}

	// 覆盖率分子分母：A→x 采样 10 / 全量 20；C→q 无连接事件 → 0。
	byKey := map[string]ExfilRow{}
	for _, r := range rows {
		byKey[r.Process+"→"+r.Host] = r
	}
	if got := byKey["A→x"]; got.Sampled != 10 || got.Logged != 20 {
		t.Errorf("A→x 覆盖率应为 10/20（分母取自 connections），得 %d/%d", got.Sampled, got.Logged)
	}
	if got := byKey["C→q"]; got.Logged != 0 {
		t.Errorf("无连接事件的通道 Logged 应为 0，得 %d", got.Logged)
	}
	// 低覆盖率（50%）的 A→x 仍在结果里——门槛只挡小样本，可信度交给 UI 披露，不隐式过滤。
	if _, ok := byKey["A→x"]; !ok {
		t.Error("低覆盖率的行不应被隐式过滤掉")
	}

	// minSampled 门槛：挡掉只有 2 条流量记录的 B→z（外发比最高但小样本噪音）。
	rows, err = s.Exfil(Filter{}, 10, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Process == "B" {
			t.Errorf("minSampled=3 应挡掉只有 2 条记录的 B→z，得 %+v", r)
		}
	}

	// minUp 门槛：只有 B→z 的上行达到 5000。
	rows, err = s.Exfil(Filter{}, 10, 5000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Process != "B" {
		t.Errorf("minUp=5000 应只剩 B→z，得 %+v", rows)
	}

	// 过滤切片同时套两张表：只看 A 的通道。
	rows, err = s.Exfil(Filter{Process: "A"}, 10, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("切片 process=A 应剩 2 条通道，得 %+v", rows)
	}
	for _, r := range rows {
		if r.Process != "A" {
			t.Errorf("切片外的通道不应出现：%+v", r)
		}
	}
	// 切片同样作用于覆盖率分母（connections 侧也带上了 process=A）。
	if rows[0].Host != "x" || rows[0].Logged != 20 {
		t.Errorf("切片内 A→x 覆盖率分母仍应为 20，得 %s/%d", rows[0].Host, rows[0].Logged)
	}

	// limit 生效。
	rows, err = s.Exfil(Filter{}, 2, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("limit=2 应返回 2 条，得 %d", len(rows))
	}
}

// TestStore_newChannels 覆盖新鲜度：首次出现取**全库** min 而非窗口内 min（本测试的核心）、
// 窗口边界、徽章判定（经出境节点 / 明文 80）、空进程排除、连接数统计、空库不报错。
func TestStore_newChannels(t *testing.T) {
	now := time.Now()
	ev := func(proc, host string, port int, node string, ago time.Duration) Event {
		return Event{
			TS: now.Add(-ago), Process: proc, Network: "tcp",
			Host: host, Port: port, Rule: "r", Node: node, Region: "hk",
		}
	}

	// 空库：空切片（非 nil）、不报错。
	if chs, err := seed(t, nil).NewChannels(24*time.Hour, 0); err != nil || chs == nil || len(chs) != 0 {
		t.Fatalf("空库应返回空切片，得 %+v（err %v）", chs, err)
	}

	s := seed(t, []Event{
		// A→old.com：48h 前就出现过，窗口内也仍在连——首次出现在窗口外，**不算新**。
		// 这是本测试的核心：若误用「窗口内 min(ts)」，它会被错报为新增。
		ev("A", "old.com", 443, "🇺🇸US", 48*time.Hour),
		ev("A", "old.com", 443, "🇺🇸US", 1*time.Hour),
		// A→new.com：首次出现在 2h 前 → 24h 窗内算新；两条连接 → count=2。
		ev("A", "new.com", 443, "🇺🇸US", 2*time.Hour),
		ev("A", "new.com", 443, "🇺🇸US", 1*time.Hour),
		// B→mid.com：30h 前首次 → 24h 窗外、7d 窗内（窗口边界用例）。
		ev("B", "mid.com", 443, "🇺🇸US", 30*time.Hour),
		// C→direct.com：只走直连 → 无「经出境节点」徽章。
		ev("C", "direct.com", 443, "DIRECT", 1*time.Hour),
		// D→plain.com：明文 80 且经出境节点 → 两个徽章都亮。
		ev("D", "plain.com", 80, "🇯🇵JP", 1*time.Hour),
		// 空进程：构不成应用通道，须排除。
		ev("", "noproc.com", 443, "🇺🇸US", 1*time.Hour),
	})

	chs, err := s.NewChannels(24*time.Hour, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]NewChannel{}
	for _, c := range chs {
		if c.Process == "" {
			t.Errorf("空进程不应出现：%+v", c)
		}
		got[c.Process+"→"+c.Host] = c
	}

	if _, ok := got["A→old.com"]; ok {
		t.Error("A→old.com 首次出现在窗口外（48h 前），不应算新增——首次出现须取全库 min(ts)，非窗口内 min")
	}
	if _, ok := got["B→mid.com"]; ok {
		t.Error("B→mid.com 首次出现在 30h 前，不应进 24h 窗")
	}
	if len(chs) != 3 {
		t.Fatalf("24h 窗应有 3 条新增（A→new.com / C→direct.com / D→plain.com），得 %d：%+v", len(chs), chs)
	}

	if c := got["A→new.com"]; c.Count != 2 || !c.Proxied || c.Plaintext {
		t.Errorf("A→new.com 应为 2 条连接、经出境节点、非明文，得 %+v", c)
	}
	if c := got["C→direct.com"]; c.Proxied {
		t.Errorf("C→direct.com 只走直连，不应标「经出境节点」，得 %+v", c)
	}
	if c := got["D→plain.com"]; !c.Plaintext || !c.Proxied {
		t.Errorf("D→plain.com 应同时标明文与经出境节点，得 %+v", c)
	}
	if c := got["A→new.com"]; c.FirstTS.After(now.Add(-90 * time.Minute)) {
		t.Errorf("A→new.com 首次时刻应是 2h 前那条（取最早），得 %v", c.FirstTS)
	}

	// 7d 窗：老通道 A→old.com（48h 前首次）与 B→mid.com 都进来了。
	chs7, err := s.NewChannels(7*24*time.Hour, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(chs7) != 5 {
		t.Errorf("7d 窗应有 5 条新增（含 old/mid），得 %d：%+v", len(chs7), chs7)
	}

	// limit 生效。
	if chs, err := s.NewChannels(7*24*time.Hour, 2); err != nil || len(chs) != 2 {
		t.Errorf("limit=2 应返回 2 条，得 %d（err %v）", len(chs), err)
	}
}

// TestStore_effectiveNewChannelsLimit 钉住上限归一逻辑——serve 层用同一函数判断结果是否被截断，
// 两边算法必须是同一个（否则「truncated」会说谎）。
func TestStore_effectiveNewChannelsLimit(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, DefaultNewChannelsLimit},
		{-1, DefaultNewChannelsLimit},
		{50, 50},
		{MaxNewChannelsLimit + 1, MaxNewChannelsLimit},
	}
	for _, c := range cases {
		if got := EffectiveNewChannelsLimit(c.in); got != c.want {
			t.Errorf("EffectiveNewChannelsLimit(%d) = %d，应为 %d", c.in, got, c.want)
		}
	}
}

// TestStore_coverage 覆盖观测覆盖：从连接密度反推——最早/最晚、有数据的小时数、空洞检测。
func TestStore_coverage(t *testing.T) {
	// 空库：时间为 nil、无空洞、不报错。
	cov, err := seed(t, nil).Coverage()
	if err != nil {
		t.Fatal(err)
	}
	if cov.Earliest != nil || cov.Latest != nil || cov.CoveredHours != 0 || len(cov.Gaps) != 0 {
		t.Fatalf("空库应为零值覆盖，得 %+v", cov)
	}

	// 小时 0、1 有数据，2~4 无（记录中断），5 恢复 → 一个 4 小时空洞、3 个有数据的小时。
	base := time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC)
	ev := func(h int) Event {
		return Event{
			TS: base.Add(time.Duration(h) * time.Hour), Process: "p", Network: "tcp",
			Host: "x.com", Port: 443, Rule: "r", Node: "n", Region: "hk",
		}
	}
	cov, err = seed(t, []Event{ev(0), ev(0), ev(1), ev(5)}).Coverage()
	if err != nil {
		t.Fatal(err)
	}
	if cov.CoveredHours != 3 {
		t.Errorf("应有 3 个有数据的小时桶，得 %d", cov.CoveredHours)
	}
	if cov.Earliest == nil || !cov.Earliest.Equal(base) {
		t.Errorf("最早应为 %v，得 %v", base, cov.Earliest)
	}
	if len(cov.Gaps) != 1 {
		t.Fatalf("应检出 1 个空洞，得 %d：%+v", len(cov.Gaps), cov.Gaps)
	}
	g := cov.Gaps[0]
	if g.Hours != 4 {
		t.Errorf("空洞应为 4 小时（1 点 → 5 点），得 %d", g.Hours)
	}
	if !g.Start.Equal(base.Truncate(time.Hour).Add(time.Hour)) {
		t.Errorf("空洞起点应为 1 点整（最后一个有数据的小时桶），得 %v", g.Start)
	}

	// 连续小时不算空洞（阈值是「相隔超过 1 小时」）。
	cov, err = seed(t, []Event{ev(0), ev(1), ev(2)}).Coverage()
	if err != nil {
		t.Fatal(err)
	}
	if len(cov.Gaps) != 0 {
		t.Errorf("连续三小时不应有空洞，得 %+v", cov.Gaps)
	}
}

// TestStore_fallthrough 覆盖「兜底连接」口径与聚合：Match/FINAL/doesn't match any rule 都算、
// 空串（GLOBAL/specialProxy）单独计数不进清单、字节按 host 关联 traffic、最后兜底时刻、时间窗、空库。
func TestStore_fallthrough(t *testing.T) {
	now := time.Now()
	ev := func(host, rule string, ago time.Duration) Event {
		return Event{
			TS: now.Add(-ago), Process: "p", Network: "tcp",
			Host: host, Port: 443, Rule: rule, Node: "n", Region: "hk",
		}
	}
	rec := func(host string, up, down int64) TrafficRecord {
		return TrafficRecord{
			Start: now, Process: "p", Network: "tcp", Host: host, Port: 443,
			Node: "n", Region: "hk", UpBytes: up, DownBytes: down, DurationMs: 1000,
		}
	}

	// 空库：空切片（非 nil）、bypassed 0、不报错。
	if fb, err := seedBoth(t, nil, nil).Fallthrough(0); err != nil || fb.Hosts == nil || len(fb.Hosts) != 0 || fb.Bypassed != 0 {
		t.Fatalf("空库应返回空清单，得 %+v（err %v）", fb, err)
	}

	s := seedBoth(t, []Event{
		// 三种兜底形态都要进清单。
		ev("a.com", "Match", time.Hour),
		ev("a.com", "Match", 2*time.Hour),
		ev("b.com", "FINAL", time.Hour),
		ev("c.com", "doesn't match any rule", time.Hour),
		// 命中具体规则 → 不算兜底。
		ev("d.com", "DomainSuffix(d.com)", time.Hour),
		// 空串 = GLOBAL/specialProxy，没走规则匹配 → 不进清单，只计数。
		ev("e.com", "", time.Hour),
		ev("f.com", "", time.Hour),
		// 窗口外的兜底（用于时间窗用例）。
		ev("old.com", "Match", 48*time.Hour),
	}, []TrafficRecord{
		rec("a.com", 100, 900), // 合计 1000 字节
		rec("d.com", 500, 500), // 命中规则的 host 的字节不该出现在结果里
	})

	fb, err := s.Fallthrough(0) // 全部历史
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]FallthroughHost{}
	for _, h := range fb.Hosts {
		got[h.Host] = h
	}
	for _, want := range []string{"a.com", "b.com", "c.com", "old.com"} {
		if _, ok := got[want]; !ok {
			t.Errorf("%s 应算兜底连接，却不在清单里", want)
		}
	}
	for _, unwanted := range []string{"d.com", "e.com", "f.com"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("%s 不该进兜底清单（命中具体规则 / 未经规则匹配）", unwanted)
		}
	}
	if fb.Bypassed != 2 {
		t.Errorf("未经规则匹配的连接应为 2 条（e.com/f.com），得 %d", fb.Bypassed)
	}
	if a := got["a.com"]; a.Conns != 2 || a.Bytes != 1000 {
		t.Errorf("a.com 应为 2 条连接、1000 字节（按 host 关联 traffic），得 conns=%d bytes=%d", a.Conns, a.Bytes)
	}
	if b := got["b.com"]; b.Bytes != 0 {
		t.Errorf("无流量记录的 host 字节应为 0，得 %d", b.Bytes)
	}
	// 最后兜底时刻取最晚的那条（a.com 的两条分别是 1h / 2h 前）。
	if a := got["a.com"]; a.LastTS.Before(now.Add(-90 * time.Minute)) {
		t.Errorf("a.com 的最后兜底时刻应是 1h 前那条，得 %v", a.LastTS)
	}

	// 时间窗：24h 内不含 48h 前的 old.com。
	fb, err = s.Fallthrough(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range fb.Hosts {
		if h.Host == "old.com" {
			t.Error("old.com 在 24h 窗外，不该出现")
		}
	}
	if len(fb.Hosts) != 3 {
		t.Errorf("24h 窗内应有 3 个兜底 host，得 %d：%+v", len(fb.Hosts), fb.Hosts)
	}
}
