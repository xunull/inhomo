package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// direct/proxied 的 SQL 谓词是单一事实源：Summary 的 FILTER 子聚合与 Filter.drillConds 共用，
// 避免「经代理」口径在两处逐字漂移。口径同 detect.isEgressNode。
const (
	sqlDirect  = "node = 'DIRECT'"
	sqlProxied = "node <> '' AND node <> 'DIRECT' AND node NOT LIKE 'REJECT%'"
)

// Filter 是一个「过滤切片」的约束集（见 CONTEXT 术语）：精确维度取值 + 直连/经代理谓词 + 时间窗。
// 所有分析查询都以某个 Filter 为范围；零值 Filter = 全集（TimeSeries 例外，内部默认近 1h）。
// 精确维度用具名字段而非 map[列名]，故列名是编译期常量、天然免注入——这是比 aggDimensions
// 那套「运行期字符串白名单」更强的约束（可过滤维度有意只是 aggDimensions 的子集，不含 rule）。
type Filter struct {
	Host    string
	Process string
	Node    string
	Region  string
	Port    *int          // nil = 不限端口
	Route   string        // "" | "direct" | "proxied"
	Since   time.Duration // >0 只统计最近 since；0 = 不限时间
}

// drillConds 构造「钻取约束」（不含时间窗）：精确列相等 + route 谓词。
// 精确列名是代码内常量、值走参数占位，防注入；route 口径与 Summary 共用同一谓词常量。
func (f Filter) drillConds() ([]string, []any) {
	var conds []string
	var args []any
	add := func(col, val string) {
		if val != "" {
			conds = append(conds, col+" = ?")
			args = append(args, val)
		}
	}
	add("host", f.Host)
	add("process", f.Process)
	add("node", f.Node)
	add("region", f.Region)
	if f.Port != nil {
		conds = append(conds, "port = ?")
		args = append(args, *f.Port)
	}
	switch f.Route {
	case "direct":
		conds = append(conds, sqlDirect)
	case "proxied":
		conds = append(conds, sqlProxied)
	}
	return conds, args
}

// where 构造完整 WHERE（钻取约束 + 可选时间窗）；无任何约束时返回空串。
func (f Filter) where() (string, []any) {
	conds, args := f.drillConds()
	if f.Since > 0 {
		conds = append(conds, "ts >= ?")
		args = append(args, time.Now().Add(-f.Since))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

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

// Summary 一次查出（过滤切片内的）总量与分布（空结果返回全 0、时间为 null，不报错）。
func (s *Store) Summary(f Filter) (Summary, error) {
	where, args := f.where()
	// direct/proxied 谓词用共享常量拼接（代码常量、无用户输入，免注入）。
	q := `SELECT
		count(*),
		count(DISTINCT host),
		count(DISTINCT nullif(process,'')),
		count(DISTINCT node),
		count(*) FILTER (WHERE ` + sqlDirect + `),
		count(*) FILTER (WHERE ` + sqlProxied + `),
		count(*) FILTER (WHERE port = 80),
		count(*) FILTER (WHERE port = 443),
		min(ts), max(ts)
		FROM connections ` + where

	var sm Summary
	var earliest, latest sql.NullTime
	err := s.DB().QueryRow(q, args...).Scan(
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

// Aggregate 在过滤切片内按 by 维度分组计数取 top-N（count 降序）。
// by 必须在白名单内（否则 ErrBadDimension）；limit<=0 或过大用默认 50。
func (s *Store) Aggregate(by string, f Filter, limit int) ([]AggRow, error) {
	col, ok := aggDimensions[by]
	if !ok {
		return nil, fmt.Errorf("%w %q（可选：%s）", ErrBadDimension, by, dimensionHint())
	}
	if limit <= 0 {
		limit = 50
	} else if limit > 1000 {
		limit = 1000 // 超上限钳到上限（维度总览取全量排名时会传大 limit）
	}
	where, args := f.where()
	// col 来自白名单、limit 已校验为整数，内插安全；过滤值用参数占位。
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

// TSPoint 是时间序列的一个点：桶起始时刻 + 该桶内连接数。
type TSPoint struct {
	TS    time.Time `json:"ts"`
	Count int64     `json:"count"`
}

// TimeSeries 把过滤切片内最近 since 的连接按 bucket 时间桶计数，按时间升序返回。
// f.Since<=0 默认 1h；bucket<=0 默认 1m。用 epoch 算术对齐分桶（不依赖 time_bucket 等专用函数）。
func (s *Store) TimeSeries(f Filter, bucket time.Duration) ([]TSPoint, error) {
	since := f.Since
	if since <= 0 {
		since = time.Hour
	}
	if bucket <= 0 {
		bucket = time.Minute
	}
	bucketSecs := max(int64(bucket/time.Second), 1)

	// 时间窗始终存在（默认 1h），叠加钻取约束。
	conds, args := f.drillConds()
	conds = append(conds, "ts >= ?")
	args = append(args, time.Now().Add(-since))
	where := "WHERE " + strings.Join(conds, " AND ")

	// bucketSecs 是校验过的整数，内插安全；过滤值用参数占位。
	q := fmt.Sprintf(`SELECT to_timestamp(floor(epoch(ts)/%d)*%d) AS b, count(*)
		FROM connections %s GROUP BY 1 ORDER BY 1`, bucketSecs, bucketSecs, where)
	rows, err := s.DB().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TSPoint{}
	for rows.Next() {
		var p TSPoint
		if err := rows.Scan(&p.TS, &p.Count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ConnRow 是原始连接明细的一行（对应 connections 表全字段）。
type ConnRow struct {
	TS      time.Time `json:"ts"`
	Process string    `json:"process"`
	Network string    `json:"network"`
	Host    string    `json:"host"`
	Port    int       `json:"port"`
	Rule    string    `json:"rule"`
	Node    string    `json:"node"`
	Region  string    `json:"region"`
}

// ConnPage 是一页明细：当前页的行 + 该过滤切片的总条数（供分页器显示「共 N 条」）。
type ConnPage struct {
	Rows  []ConnRow `json:"rows"`
	Total int64     `json:"total"`
}

// Connections 返回过滤切片内的原始连接明细，按时间倒序、offset/limit 分页，并附总条数。
// limit<=0 或过大用默认 50、上限 200；offset<0 归 0。
func (s *Store) Connections(f Filter, offset, limit int) (ConnPage, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > 200 {
		limit = 200 // 超上限钳到上限，而非跌回默认
	}
	if offset < 0 {
		offset = 0
	}
	where, args := f.where()

	var total int64
	if err := s.DB().QueryRow(`SELECT count(*) FROM connections `+where, args...).Scan(&total); err != nil {
		return ConnPage{}, err
	}

	// limit/offset 已校验为整数，内插安全；过滤值用参数占位。
	q := fmt.Sprintf(`SELECT ts, process, network, host, port, rule, node, region
		FROM connections %s ORDER BY ts DESC LIMIT %d OFFSET %d`, where, limit, offset)
	rows, err := s.DB().Query(q, args...)
	if err != nil {
		return ConnPage{}, err
	}
	defer rows.Close()

	out := []ConnRow{}
	for rows.Next() {
		var r ConnRow
		var port int32
		if err := rows.Scan(&r.TS, &r.Process, &r.Network, &r.Host, &port, &r.Rule, &r.Node, &r.Region); err != nil {
			return ConnPage{}, err
		}
		r.Port = int(port)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return ConnPage{}, err
	}
	return ConnPage{Rows: out, Total: total}, nil
}
