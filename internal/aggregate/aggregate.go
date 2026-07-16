// Package aggregate 按 (出境节点, 目的 host) 键在时间窗内去重，让终端「只冒一次、不刷屏」。
// 时钟通过 Observe 的 now 参数注入，逻辑为纯函数式、便于表驱动测试（Seam 2）。
// 注意：它只影响终端显示；原始层 JSONL 仍逐条落盘、一条不漏。
package aggregate

import "time"

// Key 是聚合键：一个 (出境节点, 目的 host) 组合。用结构体做 map 键，
// 免去手工拼分隔符，也杜绝 host 内含分隔符导致的撞键。
type Key struct {
	Node string
	Host string
}

// Aggregator 维护每个键当前时间窗的开始时刻与窗内计数。
type Aggregator struct {
	window  time.Duration
	buckets map[Key]bucket
}

type bucket struct {
	start time.Time // 当前窗口开始时刻（= 该窗首个事件的 now）
	count int       // 当前窗口内累计事件数（含首个已冒泡的）
}

// New 返回一个窗口为 window 的聚合器。
func New(window time.Duration) *Aggregator {
	return &Aggregator{window: window, buckets: map[Key]bucket{}}
}

// Observe 记录 k 在 now 的一次出现，返回：
//   - emit：是否应当在终端冒泡（首次出现，或已跨出上一窗口）
//   - suppressed：仅当因「跨窗」而冒泡时，为上一窗口内**被抑制（未显示）**的次数
//     （= 上一窗总数减去那条已冒泡的首条）；首次出现为 0
func (a *Aggregator) Observe(k Key, now time.Time) (emit bool, suppressed int) {
	b, ok := a.buckets[k]
	switch {
	case !ok:
		// 首次出现 → 开窗、冒泡。
		a.buckets[k] = bucket{start: now, count: 1}
		return true, 0
	case now.Sub(b.start) >= a.window:
		// 已跨出上一窗口 → 带上一窗「被抑制数」重新冒泡、开新窗。
		a.buckets[k] = bucket{start: now, count: 1}
		return true, b.count - 1
	default:
		// 仍在窗口内 → 抑制、累计。
		b.count++
		a.buckets[k] = b
		return false, 0
	}
}

// Distinct 返回至今出现过的不同键数量（即去重后的 (节点,host) 组合数）。
func (a *Aggregator) Distinct() int {
	return len(a.buckets)
}
