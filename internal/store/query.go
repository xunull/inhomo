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

// whereOn 构造完整 WHERE（钻取约束 + 可选时间窗）；时间列名由 tsCol 指定
// （connections 用 ts、traffic 用 start_ts）。tsCol 是代码内常量、免注入。无约束时返回空串。
func (f Filter) whereOn(tsCol string) (string, []any) {
	conds, args := f.drillConds()
	if f.Since > 0 {
		conds = append(conds, tsCol+" >= ?")
		args = append(args, time.Now().Add(-f.Since))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

// where 是 connections 表的 WHERE（时间列 ts）——Summary/Aggregate/Connections/Flow 共用。
func (f Filter) where() (string, []any) { return f.whereOn("ts") }

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
func dimensionHint(dims map[string]string) string {
	keys := make([]string, 0, len(dims))
	for k := range dims {
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
		return nil, fmt.Errorf("%w %q（可选：%s）", ErrBadDimension, by, dimensionHint(aggDimensions))
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

// HostCounts 返回过滤切片内**全部**去重 host 及其连接数（无 top-N 截断）。
// 供追踪器归类用：调用方在 Go 侧按 host 归类再重映射到归属公司（DuckDB 无公共后缀函数，
// 无法 SQL-side 按 eTLD+1 分组），故需全量而非 Aggregate 的 top-N。
func (s *Store) HostCounts(f Filter) ([]AggRow, error) {
	where, args := f.where()
	rows, err := s.DB().Query(`SELECT CAST(host AS VARCHAR), count(*) FROM connections `+where+` GROUP BY 1`, args...)
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

// FlowNode 是拓扑图（Sankey）的一个节点。Name 按层加维度前缀命名空间（如 "process:gh"、
// "node:🇺🇸US"），避免 ECharts 以 name 为唯一键时「同名跨层塌陷/成环」；Dim+Key 携带真实
// 钻取值（「其它」桶 Key 为 __other__、不可钻）。
type FlowNode struct {
	Name  string `json:"name"`
	Dim   string `json:"dim"`
	Key   string `json:"key"`
	Label string `json:"label"`
}

// FlowLink 是一条边（源节点 → 目标节点 的连接数）。
type FlowLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Value  int64  `json:"value"`
}

// FlowGraph 是 /api/flow 的返回：两层 App→节点 拓扑的节点与边。
type FlowGraph struct {
	Nodes []FlowNode `json:"nodes"`
	Links []FlowLink `json:"links"`
}

// flowOther 是「其它」桶的 Key（前端据此判定不可钻取）。用一个现实中不会作为进程名/节点名
// 出现的哨兵值——万一真有同名取值且进入 top-N，其边会与溢出桶合并、被标为「其它」（可接受的边界）。
const flowOther = "__other__"

// topKeys 取 totals 里连接数最高的 limit 个 key（count 降序、同分按 key 升序，确定性）。
func topKeys(totals map[string]int64, limit int) map[string]bool {
	keys := make([]string, 0, len(totals))
	for k := range totals {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if totals[keys[i]] != totals[keys[j]] {
			return totals[keys[i]] > totals[keys[j]]
		}
		return keys[i] < keys[j]
	})
	keep := make(map[string]bool, limit)
	for i := 0; i < len(keys) && i < limit; i++ {
		keep[keys[i]] = true
	}
	return keep
}

// flowMetricWeight 是 Flow 的 metric 白名单：度量 → (表, 时间列, 边权表达式)。
// count 走全量「连接事件」(connections) 数连接数；up/down/total 走「流量记录」(traffic) 按字节求和——
// 即拓扑图的字节口径与 /api/traffic 同源、同为**抽样**（短连接漏计，见 ADR-0008）。
// 三者均为编译期常量，内插进 SQL 安全（防注入）。
var flowMetricWeight = map[string]struct {
	table, tsCol, weight string
}{
	"count": {"connections", "ts", "count(*)"},
	"up":    {"traffic", "start_ts", "COALESCE(sum(up_bytes), 0)"},
	"down":  {"traffic", "start_ts", "COALESCE(sum(down_bytes), 0)"},
	"total": {"traffic", "start_ts", "COALESCE(sum(up_bytes) + sum(down_bytes), 0)"},
}

// Flow 返回过滤切片内 App(process) → 出境节点(node) 两层拓扑的边，边权由 metric 决定：
// 连接数(count，默认、来自全量连接事件) 或 字节(up/down/total，来自抽样的流量记录)。
// 每层按边权取 top-limit、其余归「其它」桶（limit<=0 默认 10、上限 50）。
// metric 空默认 count；非法 metric → ErrBadMetric。top-N + 重映射在 Go 侧算
//（此规模的 (process,node) 边只有几百，够用、好测）。
func (s *Store) Flow(f Filter, metric string, limit int) (FlowGraph, error) {
	if metric == "" {
		metric = "count"
	}
	spec, ok := flowMetricWeight[metric]
	if !ok {
		return FlowGraph{}, fmt.Errorf("%w %q（可选：count/up/down/total）", ErrBadMetric, metric)
	}
	if limit <= 0 {
		limit = 10
	} else if limit > 50 {
		limit = 50
	}
	where, args := f.whereOn(spec.tsCol)
	q := fmt.Sprintf("SELECT process, node, %s FROM %s %s GROUP BY 1, 2", spec.weight, spec.table, where)
	rows, err := s.DB().Query(q, args...)
	if err != nil {
		return FlowGraph{}, err
	}
	defer rows.Close()

	type pair struct{ app, node string }
	edges := map[pair]int64{}
	appTotal := map[string]int64{}
	nodeTotal := map[string]int64{}
	for rows.Next() {
		var app, node string
		var cnt int64
		if err := rows.Scan(&app, &node, &cnt); err != nil {
			return FlowGraph{}, err
		}
		edges[pair{app, node}] += cnt
		appTotal[app] += cnt
		nodeTotal[node] += cnt
	}
	if err := rows.Err(); err != nil {
		return FlowGraph{}, err
	}

	appTop := topKeys(appTotal, limit)
	nodeTop := topKeys(nodeTotal, limit)

	// 重映射：非 top 的取值折进「其它」，累加同一对边。
	merged := map[pair]int64{}
	for p, c := range edges {
		app, node := p.app, p.node
		if !appTop[app] {
			app = flowOther
		}
		if !nodeTop[node] {
			node = flowOther
		}
		merged[pair{app, node}] += c
	}

	nodeIndex := map[string]FlowNode{}
	addNode := func(dim, key string) string {
		name := dim + ":" + key
		if _, ok := nodeIndex[name]; !ok {
			label := key
			switch key {
			case "":
				label = "(未知)"
			case flowOther:
				label = "其它"
			}
			nodeIndex[name] = FlowNode{Name: name, Dim: dim, Key: key, Label: label}
		}
		return name
	}

	links := make([]FlowLink, 0, len(merged))
	for p, c := range merged {
		src := addNode("process", p.app)
		dst := addNode("node", p.node)
		links = append(links, FlowLink{Source: src, Target: dst, Value: c})
	}
	// 确定性排序（value 降序，再按 source/target），便于测试与稳定渲染。
	sort.Slice(links, func(i, j int) bool {
		if links[i].Value != links[j].Value {
			return links[i].Value > links[j].Value
		}
		if links[i].Source != links[j].Source {
			return links[i].Source < links[j].Source
		}
		return links[i].Target < links[j].Target
	})

	nodes := make([]FlowNode, 0, len(nodeIndex))
	for _, n := range nodeIndex {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Dim != nodes[j].Dim {
			return nodes[i].Dim < nodes[j].Dim
		}
		return nodes[i].Name < nodes[j].Name
	})

	return FlowGraph{Nodes: nodes, Links: links}, nil
}

// TrafficRow 是带宽聚合的一行：某维度取值的上/下行字节合计。
type TrafficRow struct {
	Key       string `json:"key"`
	UpBytes   int64  `json:"up"`
	DownBytes int64  `json:"down"`
}

// TrafficAgg 是 /api/traffic 的返回：过滤切片内按维度的字节 top-N + 该切片总上/下行。
type TrafficAgg struct {
	Rows      []TrafficRow `json:"rows"`
	TotalUp   int64        `json:"totalUp"`
	TotalDown int64        `json:"totalDown"`
}

// ErrBadMetric 表示 metric 不在 up/down/total 内。
var ErrBadMetric = errors.New("不支持的度量")

// trafficDimensions 是 traffic 表的 by 白名单（防注入）：与 aggDimensions「维度一致」，仅去掉 rule——
// traffic 表无 rule 列，若放进白名单，by=rule 会落到不存在的列而 500；挡在白名单外则返回 400 更诚实。
var trafficDimensions = map[string]string{
	"host":    "host",
	"process": "process",
	"node":    "node",
	"region":  "region",
	"port":    "port",
}

// trafficMetricOrder 是 metric 白名单：度量 → ORDER BY 表达式（驱动 top-N 排序）。
// 上/下行两列始终一并返回，仅排序键随 metric 变，故前端切换 metric 不必重取两份字节。
var trafficMetricOrder = map[string]string{
	"up":    "sum(up_bytes)",
	"down":  "sum(down_bytes)",
	"total": "sum(up_bytes) + sum(down_bytes)",
}

// Traffic 在流量记录之上、按 by 维度聚合上/下行字节取 top-N（按 metric 排序），并返回该切片总上/下行。
// by 须在 trafficDimensions 白名单内（否则 ErrBadDimension）；metric 空默认 total、非法 ErrBadMetric；
// limit<=0 默认 20、上限 200。空结果 rows 为空切片（非 nil）、总量 0，不报错。
func (s *Store) Traffic(by, metric string, f Filter, limit int) (TrafficAgg, error) {
	col, ok := trafficDimensions[by]
	if !ok {
		return TrafficAgg{}, fmt.Errorf("%w %q（可选：%s）", ErrBadDimension, by, dimensionHint(trafficDimensions))
	}
	if metric == "" {
		metric = "total"
	}
	orderExpr, ok := trafficMetricOrder[metric]
	if !ok {
		return TrafficAgg{}, fmt.Errorf("%w %q（可选：up/down/total）", ErrBadMetric, metric)
	}
	if limit <= 0 {
		limit = 20
	} else if limit > 200 {
		limit = 200
	}
	where, args := f.whereOn("start_ts")

	// 切片总上/下行（空结果 sum 为 NULL，COALESCE 归 0）。
	ag := TrafficAgg{Rows: []TrafficRow{}}
	if err := s.DB().QueryRow(
		`SELECT COALESCE(sum(up_bytes),0), COALESCE(sum(down_bytes),0) FROM traffic `+where, args...,
	).Scan(&ag.TotalUp, &ag.TotalDown); err != nil {
		return TrafficAgg{}, err
	}

	// col 来自白名单、orderExpr 来自白名单、limit 已校验整数，内插安全；过滤值用参数占位。
	q := fmt.Sprintf(`SELECT CAST(%s AS VARCHAR), COALESCE(sum(up_bytes),0), COALESCE(sum(down_bytes),0)
		FROM traffic %s GROUP BY 1 ORDER BY %s DESC, 1 LIMIT %d`, col, where, orderExpr, limit)
	rows, err := s.DB().Query(q, args...)
	if err != nil {
		return TrafficAgg{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var r TrafficRow
		if err := rows.Scan(&r.Key, &r.UpBytes, &r.DownBytes); err != nil {
			return TrafficAgg{}, err
		}
		ag.Rows = append(ag.Rows, r)
	}
	return ag, rows.Err()
}

// ExfilRow 是一条「应用通道」的外发账（见 CONTEXT 术语「应用通道」/「外发比」/「采样覆盖率」）。
// Sampled/Logged 是覆盖率的原始分子分母（分别来自抽样的 traffic 与全量的 connections），
// 两个原始数都给出而不只给比值——UI 要能说清「87 条里只采到 12 条」，而非一个干巴巴的 14%。
type ExfilRow struct {
	Process   string  `json:"process"`
	Host      string  `json:"host"`
	UpBytes   int64   `json:"up"`
	DownBytes int64   `json:"down"`
	Ratio     float64 `json:"ratio"`   // 外发比 = 上行 / max(下行,1)；与 ORDER BY 同一个表达式算出，不会漂移
	Sampled   int64   `json:"sampled"` // 该通道在 traffic（抽样）中的行数
	Logged    int64   `json:"logged"`  // 该通道在 connections（全量）中的行数
}

// 外发比的默认门槛（见 ADR-0013）：门槛只挡「小样本噪音」，不挡「低覆盖率」——
// 前者是「这行数字有没有意义」，后者是「这行数字可不可信」，后者该披露而非过滤。
const (
	defaultExfilMinUp      = 5 << 20 // 5 MB
	defaultExfilMinSampled = 10      // 至少 10 条流量记录
)

// withNonEmptyProcess 往 whereOn 产出的 WHERE 上再挂一个「进程非空」条件。
// 空进程（mihomo 自身 / 未开 find-process）无法构成「应用通道」——说不出是谁在传，这行就没有意义。
func withNonEmptyProcess(where string) string {
	if where == "" {
		return "WHERE process <> ''"
	}
	return where + " AND process <> ''"
}

// Exfil 在过滤切片内按「应用通道」(process, host) 聚合上/下行字节，按「外发比」降序取 top-N，
// 并 LEFT JOIN 全量 connections 给出每行的「采样覆盖率」原始分子分母。
//
// 与 Traffic 分开而不复用（见 ADR-0013）：行的形状不同（两列主键）、要跨表 JOIN、有自己的门槛参数。
// limit<=0 默认 20、上限 200；minUp/minSampled <=0 取默认门槛（要放开就显式传 1）。
// 空结果返回空切片（非 nil），不报错。
func (s *Store) Exfil(f Filter, limit int, minUp int64, minSampled int) ([]ExfilRow, error) {
	if limit <= 0 {
		limit = 20
	} else if limit > 200 {
		limit = 200
	}
	if minUp <= 0 {
		minUp = defaultExfilMinUp
	}
	if minSampled <= 0 {
		minSampled = defaultExfilMinSampled
	}
	// 同一个切片套两张表：traffic 的时间列是 start_ts、connections 的是 ts。
	whereT, argsT := f.whereOn("start_ts")
	whereC, argsC := f.whereOn("ts")

	// 表名/时间列/limit 都是代码内常量或已校验整数，内插安全；过滤值与门槛走参数占位。
	// 比值在 SQL 里算一次，既用于 ORDER BY 也被 SELECT 出来 —— 排序与展示同源，不会各算各的。
	q := fmt.Sprintf(`WITH t AS (
			SELECT process, host, sum(up_bytes) AS up, sum(down_bytes) AS down, count(*) AS sampled
			FROM traffic %s GROUP BY 1, 2
		), c AS (
			SELECT process, host, count(*) AS logged
			FROM connections %s GROUP BY 1, 2
		)
		SELECT t.process, t.host, t.up, t.down,
			t.up * 1.0 / greatest(t.down, 1) AS ratio,
			t.sampled, COALESCE(c.logged, 0)
		FROM t LEFT JOIN c ON c.process = t.process AND c.host = t.host
		WHERE t.up >= ? AND t.sampled >= ?
		ORDER BY ratio DESC, t.up DESC, t.process, t.host
		LIMIT %d`,
		withNonEmptyProcess(whereT), withNonEmptyProcess(whereC), limit)

	args := make([]any, 0, len(argsT)+len(argsC)+2)
	args = append(args, argsT...)
	args = append(args, argsC...)
	args = append(args, minUp, minSampled)

	rows, err := s.DB().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ExfilRow{}
	for rows.Next() {
		var r ExfilRow
		if err := rows.Scan(&r.Process, &r.Host, &r.UpBytes, &r.DownBytes, &r.Ratio, &r.Sampled, &r.Logged); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// NewChannel 是一条「新增应用通道」（见 CONTEXT 术语）：某 (App, host) 在**全库历史**中的
// 最早连接事件恰好落在当前时间窗内。Proxied/Plaintext 是给 UI 打徽章用的风险标记——
// 只标不滤（实测 24h 内同时明文+经出境节点的新增仅 2 条，做成过滤条件会得到空页面，见 ADR-0014）。
type NewChannel struct {
	Process   string    `json:"process"`
	Host      string    `json:"host"`
	FirstTS   time.Time `json:"firstTs"`
	Count     int64     `json:"count"`     // 窗口内该通道的连接数
	Proxied   bool      `json:"proxied"`   // 有任一连接经出境节点
	Plaintext bool      `json:"plaintext"` // 有任一连接目的端口为 80
}

// 新增通道结果集的规模上限。导出是为了让调用方能用同一函数算出「生效上限」，
// 从而判断结果是否被截断并如实告知——静默截断会让用户把「只列了这些」读成「只有这些」。
const (
	DefaultNewChannelsLimit = 2000
	MaxNewChannelsLimit     = 10000
)

// EffectiveNewChannelsLimit 把请求的 limit 归一到实际生效值（<=0 取默认、超上限钳到上限）。
func EffectiveNewChannelsLimit(limit int) int {
	if limit <= 0 {
		return DefaultNewChannelsLimit
	}
	if limit > MaxNewChannelsLimit {
		return MaxNewChannelsLimit
	}
	return limit
}

// NewChannels 返回「首次出现落在最近 since 内」的应用通道，按 (App, 首次时刻倒序) 排列。
//
// 不接受 Filter（见 CONTEXT「过滤切片」的边界说明）：首次出现要拿**窗口外的历史**当参照，
// 而切片只会缩小可见范围——若把 region=JP 之类的约束套进来，min(ts) 会漂移成
// 「在 JP 首次出现的时刻」，语义就变了。故这里只收时间窗。
//
// since<=0 默认 24h；limit 按 EffectiveNewChannelsLimit 归一（调用方用同一函数算出生效上限，
// 再据 len(rows) >= 上限 判断结果是否被截断——不静默截断）。
func (s *Store) NewChannels(since time.Duration, limit int) ([]NewChannel, error) {
	if since <= 0 {
		since = 24 * time.Hour
	}
	limit = EffectiveNewChannelsLimit(limit)
	cutoff := time.Now().Add(-since)

	// firsts 取全库最早时刻（无时间条件——这正是「新」的参照系）；外层再筛出落在窗口内的。
	// JOIN 无需再限 c.ts >= cutoff：first_ts >= cutoff 已蕴含该通道的每条连接都在窗口内。
	q := fmt.Sprintf(`WITH firsts AS (
			SELECT process, host, min(ts) AS first_ts
			FROM connections WHERE process <> '' GROUP BY 1, 2
		)
		SELECT f.process, f.host, f.first_ts, count(*),
			count(*) FILTER (WHERE %s) > 0,
			count(*) FILTER (WHERE c.port = 80) > 0
		FROM firsts f
		JOIN connections c ON c.process = f.process AND c.host = f.host
		WHERE f.first_ts >= ?
		GROUP BY 1, 2, 3
		ORDER BY f.process, f.first_ts DESC, f.host
		LIMIT %d`, sqlProxied, limit)

	rows, err := s.DB().Query(q, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []NewChannel{}
	for rows.Next() {
		var c NewChannel
		if err := rows.Scan(&c.Process, &c.Host, &c.FirstTS, &c.Count, &c.Proxied, &c.Plaintext); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CoverageGap 是一段「没在记录」的空洞：从 Start 那个小时之后到 End 那个小时之前。
type CoverageGap struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Hours int64     `json:"hours"`
}

// Coverage 是「观测覆盖」（见 CONTEXT 术语）：库中确实处于记录状态的时间范围与其空洞。
type Coverage struct {
	Earliest     *time.Time    `json:"earliest"`
	Latest       *time.Time    `json:"latest"`
	CoveredHours int64         `json:"coveredHours"` // 有连接事件的小时桶数
	Gaps         []CoverageGap `json:"gaps"`
}

// coverageGapHours 是判定空洞的阈值：相邻两个「有数据的小时」相距超过它即视为中断。
// 取 1 小时的依据是实测——有数据的小时里最少的一个也有 245 条连接、中位数 2765，
// 故「整整一小时零连接」几乎必然是没在记录，而非那小时没上网（见 ADR-0014）。
const coverageGapHours = 1

// Coverage 从连接密度反推观测覆盖，不依赖任何独立的心跳记录（见 ADR-0014）。
// 空库返回零值（时间为 nil、无空洞），不报错。
func (s *Store) Coverage() (Coverage, error) {
	var cov Coverage
	var earliest, latest sql.NullTime
	if err := s.DB().QueryRow(
		`SELECT min(ts), max(ts), count(DISTINCT date_trunc('hour', ts)) FROM connections`,
	).Scan(&earliest, &latest, &cov.CoveredHours); err != nil {
		return Coverage{}, err
	}
	if earliest.Valid {
		cov.Earliest = &earliest.Time
	}
	if latest.Valid {
		cov.Latest = &latest.Time
	}

	cov.Gaps = []CoverageGap{}
	q := fmt.Sprintf(`WITH b AS (
			SELECT date_trunc('hour', ts) AS h FROM connections GROUP BY 1
		), o AS (
			SELECT h, lag(h) OVER (ORDER BY h) AS prev FROM b
		)
		SELECT prev, h, date_diff('hour', prev, h)
		FROM o WHERE prev IS NOT NULL AND date_diff('hour', prev, h) > %d
		ORDER BY prev`, coverageGapHours)
	rows, err := s.DB().Query(q)
	if err != nil {
		return Coverage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var g CoverageGap
		if err := rows.Scan(&g.Start, &g.End, &g.Hours); err != nil {
			return Coverage{}, err
		}
		cov.Gaps = append(cov.Gaps, g)
	}
	return cov, rows.Err()
}

// CountNewChannels 只数「首次出现落在窗口内」的应用通道数量，供主页 KPI 当钩子用——
// 主页每 10 秒自动刷新，不该为了一个数字把整份新增清单拉过来。
//
// excludeProcesses 用大小写不敏感的**子串**排除（口径与 serve 层的 isMutedProcess 一致），
// 好让这个数字与 /new 页面上「未折叠」的那部分对得上——否则主页说 500、页面上只看到 254，
// 用户会以为少了一半。排除发生在分组前，但 process 是分组键，故不影响其它进程的 min(ts)。
func (s *Store) CountNewChannels(since time.Duration, excludeProcesses []string) (int64, error) {
	if since <= 0 {
		since = 24 * time.Hour
	}
	conds := []string{"process <> ''"}
	args := make([]any, 0, len(excludeProcesses)+1)
	for _, p := range excludeProcesses {
		conds = append(conds, "process NOT ILIKE ?")
		args = append(args, "%"+p+"%")
	}
	args = append(args, time.Now().Add(-since))

	q := `WITH firsts AS (
			SELECT process, host, min(ts) AS first_ts
			FROM connections WHERE ` + strings.Join(conds, " AND ") + ` GROUP BY 1, 2
		)
		SELECT count(*) FROM firsts WHERE first_ts >= ?`
	var n int64
	err := s.DB().QueryRow(q, args...).Scan(&n)
	return n, err
}
