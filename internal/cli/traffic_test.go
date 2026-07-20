package cli

import (
	"testing"
	"time"

	"github.com/xunull/inhomo/internal/logstream"
)

func TestCompletedFrom(t *testing.T) {
	now := time.Now()
	start := now.Add(-10 * time.Second)
	mk := func(up, down int64) activeConn {
		return activeConn{start: start, up: up, down: down, host: "h", node: "N", process: "p", port: 443}
	}
	prev := map[string]activeConn{"id1": mk(100, 200), "id2": mk(5, 5)}
	cur := map[string]activeConn{"id2": mk(9, 9), "id3": mk(1, 1)}

	// id1 消失 → 一条记录（用 prev 的终值字节）；id2 仍在、id3 新增 → 不产出。
	recs := completedFrom(prev, cur, now)
	if len(recs) != 1 {
		t.Fatalf("应产出 1 条（id1 消失），得 %d：%+v", len(recs), recs)
	}
	if r := recs[0]; r.UpBytes != 100 || r.DownBytes != 200 {
		t.Errorf("字节应用 prev 终值：up=%d down=%d", r.UpBytes, r.DownBytes)
	}
	if recs[0].DurationMs < 9000 { // ≈10s
		t.Errorf("时长≈now−start，得 %dms", recs[0].DurationMs)
	}

	// 退出场景（空 cur）→ prev 全部落。
	if all := completedFrom(prev, map[string]activeConn{}, now); len(all) != 2 {
		t.Errorf("退出应落全部活跃 2 条，得 %d", len(all))
	}
	// 首轮（空 prev）→ 无产出；短连接从未进 prev 同理不产出。
	if none := completedFrom(map[string]activeConn{}, cur, now); len(none) != 0 {
		t.Errorf("首轮不该产出，得 %d", len(none))
	}
}

func TestToActive(t *testing.T) {
	c := logstream.Conn{
		ID: "x", Upload: 10, Download: 20, Start: time.Now(),
		Chains:   []string{"🇺🇸US", "🚀 节点选择"}, // chains[0] = 实际出境节点
		Metadata: logstream.ConnMeta{Network: "tcp", Host: "a.com", Process: "gh", DestinationPort: "443"},
	}
	a := toActive(c)
	if a.node != "🇺🇸US" {
		t.Errorf("node 应取 chains[0]，得 %q", a.node)
	}
	if a.region != "US" { // 从 node 名的国旗解析
		t.Errorf("region 从 node 解析应 US，得 %q", a.region)
	}
	if a.port != 443 || a.host != "a.com" || a.process != "gh" || a.up != 10 || a.down != 20 {
		t.Errorf("字段映射错：%+v", a)
	}
}
