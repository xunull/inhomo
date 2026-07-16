package aggregate

import (
	"testing"
	"time"
)

// TestAggregator_windowDedup：窗口内抑制、跨窗重新冒泡并带上一窗「被抑制数」。
func TestAggregator_windowDedup(t *testing.T) {
	base := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	a := New(5 * time.Minute)
	k := Key{Node: "🇺🇸 US-02", Host: "plain.example.com"}

	if emit, _ := a.Observe(k, base); !emit {
		t.Fatal("首次出现应冒泡")
	}
	if emit, _ := a.Observe(k, base.Add(1*time.Minute)); emit {
		t.Fatal("窗口内(1min)应被抑制")
	}
	if emit, _ := a.Observe(k, base.Add(4*time.Minute)); emit {
		t.Fatal("窗口内(4min)应被抑制")
	}
	emit, suppressed := a.Observe(k, base.Add(5*time.Minute))
	if !emit {
		t.Fatal("跨窗(5min)应重新冒泡")
	}
	if suppressed != 2 {
		t.Fatalf("上一窗被抑制数应为 2（首条已冒泡，之后 2 次被抑制），得 %d", suppressed)
	}
	// 新窗内再来一次 → 又被抑制
	if emit, _ := a.Observe(k, base.Add(6*time.Minute)); emit {
		t.Fatal("新窗内应被抑制")
	}
}

// TestAggregator_keysIndependent：不同键互不影响；Distinct 计不同键数。
func TestAggregator_keysIndependent(t *testing.T) {
	base := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	a := New(5 * time.Minute)
	kA := Key{Node: "node-A", Host: "a.com"}
	kB := Key{Node: "node-B", Host: "b.com"}

	if emit, _ := a.Observe(kA, base); !emit {
		t.Fatal("kA 首次应冒泡")
	}
	if emit, _ := a.Observe(kB, base); !emit {
		t.Fatal("kB 首次应冒泡（不同键互不影响）")
	}
	if emit, _ := a.Observe(kA, base.Add(1*time.Minute)); emit {
		t.Fatal("kA 窗口内应抑制")
	}
	if a.Distinct() != 2 {
		t.Fatalf("去重组合数应为 2，得 %d", a.Distinct())
	}
}
