package sink

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xunull/inhomo/internal/detect"
)

// TestNewRecord_fields 断言泄露事件到 Record 的字段映射与时间格式。
func TestNewRecord_fields(t *testing.T) {
	tm := time.Date(2026, 7, 16, 21, 46, 3, 0, time.UTC)
	leak := detect.LeakEvent{Host: "plain.example.com", Port: 80, Node: "🇺🇸 US-02", Region: "US", Rule: "DomainKeyword(example)"}
	r := NewRecord(leak, tm)
	if r.Host != leak.Host || r.Port != 80 || r.Node != leak.Node || r.Region != "US" || r.Rule != leak.Rule {
		t.Fatalf("字段映射错：%+v", r)
	}
	if r.TS != "2026-07-16T21:46:03.000Z" {
		t.Fatalf("TS 格式错：%q", r.TS)
	}
}

// TestJSONLWriter_oneLinePerEvent 断言：每事件恰好一行、每行合法 JSON、能解回、末尾有换行（一条不漏）。
func TestJSONLWriter_oneLinePerEvent(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONLWriter(&buf)
	tm := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	leaks := []detect.LeakEvent{
		{Host: "a.com", Port: 80, Node: "N1", Region: "US", Rule: "R1"},
		{Host: "b.com", Port: 80, Node: "N2", Region: "unknown", Rule: ""},
		{Host: "[::1]", Port: 80, Node: "🇭🇰 HK", Region: "HK", Rule: "R3"},
	}
	for _, l := range leaks {
		if err := w.Write(NewRecord(l, tm)); err != nil {
			t.Fatalf("Write 失败：%v", err)
		}
	}

	sc := bufio.NewScanner(strings.NewReader(buf.String()))
	n := 0
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("第 %d 行非合法 JSON：%q（%v）", n, sc.Text(), err)
		}
		n++
	}
	if n != len(leaks) {
		t.Fatalf("行数=%d，期望 %d（应一条不漏、一事件一行）", n, len(leaks))
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatal("JSONL 每行应以换行结束")
	}
}
