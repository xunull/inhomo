package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Summary 是 /api/summary 的返回：总量与几个关键分布。
type Summary struct {
	Total     int64      `json:"total"`     // 总连接事件数
	Hosts     int64      `json:"hosts"`     // 去重目的 host 数
	Processes int64      `json:"processes"` // 去重进程数（不含空）
	Nodes     int64      `json:"nodes"`     // 去重出境节点数
	Direct    int64      `json:"direct"`    // 直连（node=DIRECT）
	Proxied   int64      `json:"proxied"`   // 经真实出境节点（排除 DIRECT / REJECT* / 空，口径同 detect.isEgressNode）
	HTTP      int64      `json:"http"`      // 目的端口 80
	HTTPS     int64      `json:"https"`     // 目的端口 443
	Earliest  *time.Time `json:"earliest"`  // 最早一条（空库为 null）
	Latest    *time.Time `json:"latest"`    // 最晚一条
}

// Summary 一次查出总量与分布（空库返回全 0、时间为 null，不报错）。
func (s *Store) Summary() (Summary, error) {
	const q = `SELECT
		count(*),
		count(DISTINCT host),
		count(DISTINCT nullif(process,'')),
		count(DISTINCT node),
		count(*) FILTER (WHERE node = 'DIRECT'),
		count(*) FILTER (WHERE node <> '' AND node <> 'DIRECT' AND node NOT LIKE 'REJECT%'),
		count(*) FILTER (WHERE port = 80),
		count(*) FILTER (WHERE port = 443),
		min(ts), max(ts)
		FROM connections`

	var sm Summary
	var earliest, latest sql.NullTime
	err := s.DB().QueryRow(q).Scan(
		&sm.Total, &sm.Hosts, &sm.Processes, &sm.Nodes,
		&sm.Direct, &sm.Proxied, &sm.HTTP, &sm.HTTPS,
		&earliest, &latest,
	)
	if err != nil {
		return Summary{}, err
	}
	if earliest.Valid {
		sm.Earliest = &earliest.Time
	}
	if latest.Valid {
		sm.Latest = &latest.Time
	}
	return sm, nil
}

// AggRow 是聚合结果的一行：某维度的一个取值及其连接数。
type AggRow struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// ErrBadDimension 表示 by 维度不在白名单内。
var ErrBadDimension = errors.New("不支持的分组维度")

// aggDimensions 是 by 参数的白名单：维度名 → 实际列（只允许这些列进 SQL，防注入）。
var aggDimensions = map[string]string{
	"host":    "host",
	"process": "process",
	"node":    "node",
	"region":  "region",
	"port":    "port",
	"rule":    "rule",
}

// dimensionHint 从白名单 map 的 keys 排序生成可读列表（单一事实源，避免与错误提示漂移）。
func dimensionHint() string {
	keys := make([]string, 0, len(aggDimensions))
	for k := range aggDimensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, "/")
}

// Aggregate 按 by 维度分组计数取 top-N（count 降序）。
// by 必须在白名单内（否则 ErrBadDimension）；since>0 只统计最近 since；limit<=0 或过大用默认 50。
func (s *Store) Aggregate(by string, since time.Duration, limit int) ([]AggRow, error) {
	col, ok := aggDimensions[by]
	if !ok {
		return nil, fmt.Errorf("%w %q（可选：%s）", ErrBadDimension, by, dimensionHint())
	}
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	where, args := "", []any(nil)
	if since > 0 {
		where = "WHERE ts >= ?"
		args = append(args, time.Now().Add(-since))
	}
	// col 来自白名单、limit 已校验为整数，内插安全；时间用参数占位。
	q := fmt.Sprintf(`SELECT CAST(%s AS VARCHAR), count(*)
		FROM connections %s GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT %d`, col, where, limit)
	rows, err := s.DB().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AggRow{}
	for rows.Next() {
		var r AggRow
		if err := rows.Scan(&r.Key, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
