package store

import (
	"path/filepath"
	"testing"
	"time"
)

// TestStore_roundtrip 验证 Add→Flush→查询 的往返：行数、group-by、字段回读。
func TestStore_roundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.duckdb")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	evs := []Event{
		{TS: now, Process: "codex", Network: "TCP", Host: "chatgpt.com", Port: 443, Rule: "R", Node: "🇺🇸US|1.0X", Region: "US"},
		{TS: now, Process: "", Network: "TCP", Host: "plain.cn", Port: 80, Rule: "R", Node: "DIRECT", Region: "unknown"},
		{TS: now, Process: "chrome", Network: "TCP", Host: "example.com", Port: 80, Rule: "R", Node: "🇺🇸US|1.0X", Region: "US"},
	}
	for _, e := range evs {
		if err := s.Add(e); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	var n int
	if err := s.DB().QueryRow(`SELECT count(*) FROM connections`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("行数=%d，期望 3", n)
	}

	// group by region → US 最多
	var region string
	var c int
	if err := s.DB().QueryRow(`SELECT region, count(*) AS c FROM connections GROUP BY region ORDER BY c DESC LIMIT 1`).Scan(&region, &c); err != nil {
		t.Fatal(err)
	}
	if region != "US" || c != 2 {
		t.Fatalf("top region=%s(%d)，期望 US(2)", region, c)
	}

	// 字段回读
	var host, node string
	var port int
	if err := s.DB().QueryRow(`SELECT host, port, node FROM connections WHERE process='codex'`).Scan(&host, &port, &node); err != nil {
		t.Fatal(err)
	}
	if host != "chatgpt.com" || port != 443 || node != "🇺🇸US|1.0X" {
		t.Fatalf("字段回读错：host=%s port=%d node=%s", host, port, node)
	}
}

// TestStore_closedNoop 验证关闭后 Add 报错、Flush/Close 幂等无 panic。
func TestStore_closedNoop(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "c.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(Event{Host: "x"}); err == nil {
		t.Fatal("关闭后 Add 应报错")
	}
	if err := s.Flush(); err != nil {
		t.Fatalf("关闭后 Flush 应无操作：%v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("重复 Close 应幂等：%v", err)
	}
}
