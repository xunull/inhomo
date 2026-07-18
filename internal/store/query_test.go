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
