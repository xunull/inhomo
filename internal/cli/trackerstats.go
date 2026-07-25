package cli

import (
	"sort"

	"github.com/xunull/inhomo/internal/store"
	"github.com/xunull/inhomo/internal/tracker"
)

// OwnerCount 是一家追踪器归属公司在切片内的连接数。
type OwnerCount struct {
	Owner string `json:"owner"`
	Count int64  `json:"count"`
}

// TrackerBreakdown 是 /api/trackers 的返回：切片内连接总数、命中已知追踪器的连接数、按归属公司的 top-N。
// Tracker/Total 给出「多少连接走了已知追踪器」的占比；Owners 是 Google/Facebook 等归属排名。
// Loaded 表示追踪器数据是否已拉取——前端据此区分「没命中追踪器」与「没拉数据」，未拉时提示跑 tracker update。
type TrackerBreakdown struct {
	Total   int64        `json:"total"`
	Tracker int64        `json:"tracker"`
	Owners  []OwnerCount `json:"owners"`
	Loaded  bool         `json:"loaded"`
}

// computeTrackerBreakdown 把「host → 连接数」按归类器归并成追踪器占比 + 归属公司 top-N（纯函数、可测）。
// 命中已知追踪器的连接计入 Tracker 与其归属桶；归属名可能为空（命中但数据无归属名），由前端渲染标签
// （后端只出原始值，不掺 UI 文案）。topN<=0 表示不截断。
func computeTrackerBreakdown(hosts []store.AggRow, c *tracker.Classifier, topN int) TrackerBreakdown {
	var total, trackerConns int64
	byOwner := map[string]int64{}
	for _, h := range hosts {
		total += h.Count
		owner, known := c.Classify(h.Key)
		if !known {
			continue
		}
		trackerConns += h.Count
		byOwner[owner] += h.Count // owner 可能为 ""，作为「无归属名」桶，标签交前端
	}

	owners := make([]OwnerCount, 0, len(byOwner))
	for o, cnt := range byOwner {
		owners = append(owners, OwnerCount{Owner: o, Count: cnt})
	}
	// count 降序、同分按名升序（确定性，便于测试与稳定渲染）。
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].Count != owners[j].Count {
			return owners[i].Count > owners[j].Count
		}
		return owners[i].Owner < owners[j].Owner
	})
	if topN > 0 && len(owners) > topN {
		owners = owners[:topN]
	}
	return TrackerBreakdown{Total: total, Tracker: trackerConns, Owners: owners, Loaded: c.Len() > 0}
}
