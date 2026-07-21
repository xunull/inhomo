// inhomo 后端 3 个分析接口的 typed client。同源（serve 托管前端 + /api）。

export interface Summary {
  total: number
  hosts: number
  processes: number
  nodes: number
  direct: number
  proxied: number
  http: number
  https: number
  earliest: string | null
  latest: string | null
}

export interface AggRow {
  key: string
  count: number
}

// 后端白名单支持的聚合维度。
export type Dimension = 'host' | 'process' | 'node' | 'region' | 'port'

export interface TSPoint {
  ts: string
  count: number
}

async function getJSON<T>(url: string): Promise<T> {
  const r = await fetch(url)
  if (!r.ok) {
    let msg = `HTTP ${r.status}`
    try {
      const body = (await r.json()) as { error?: string }
      if (body.error) msg = body.error
    } catch {
      /* 非 JSON 错误体，用状态码 */
    }
    throw new Error(msg)
  }
  return (await r.json()) as T
}

// Filter 是前端的「过滤切片」：钻取约束（精确维度 + route 谓词）。时间窗 since 是 UI 控件、单独传。
export interface Filter {
  host?: string
  process?: string
  node?: string
  region?: string
  port?: number
  route?: 'direct' | 'proxied'
}

// EMPTY_FILTER 是全集切片（主面板用）；导出为模块常量以保持引用稳定，避免下游 useApi 误重取。
export const EMPTY_FILTER: Filter = {}

// FILTER_DIMS 是可过滤的精确维度描述（单一事实源）：驱动 URL 编解码、面包屑、钻取。
// route 谓词不在此表（布尔式、值需翻译成直连/经代理），各处单独处理。
export const FILTER_DIMS: { key: Dimension; label: string; numeric?: boolean }[] = [
  { key: 'host', label: '域名' },
  { key: 'process', label: 'App' },
  { key: 'node', label: '节点' },
  { key: 'region', label: '地区' },
  { key: 'port', label: '端口', numeric: true },
]

// filterParams 把过滤切片编码为 URLSearchParams（只带非空约束）。导出供单测直接验证往返编码。
export function filterParams(f: Filter): URLSearchParams {
  const p = new URLSearchParams()
  for (const d of FILTER_DIMS) {
    const v = f[d.key]
    if (v != null && v !== '') p.set(d.key, String(v))
  }
  if (f.route) p.set('route', f.route)
  return p
}

// qs 把过滤切片 + 额外参数拼成查询串（只带非空项）。导出供单测直接验证 extra 合并/丢空/前缀。
export function qs(f: Filter, extra: Record<string, string | number | undefined> = {}): string {
  const p = filterParams(f)
  for (const [k, v] of Object.entries(extra)) {
    if (v !== undefined && v !== '') p.set(k, String(v))
  }
  const s = p.toString()
  return s ? `?${s}` : ''
}

// filterFromParams：URL 查询参数 → Filter（详情页从 URL 还原过滤切片）。
export function filterFromParams(p: URLSearchParams): Filter {
  const f: Filter = {}
  for (const d of FILTER_DIMS) {
    if (d.numeric) continue // port 单独处理（需转数字）
    const v = p.get(d.key)
    if (v) (f as Record<string, string>)[d.key] = v
  }
  const port = p.get('port')
  if (port && !Number.isNaN(Number(port))) f.port = Number(port)
  const route = p.get('route')
  if (route === 'direct' || route === 'proxied') f.route = route
  return f
}

// pathWith：构造带过滤切片 + 可选时间窗的页面 URL（detail/topology 共用同一编码逻辑）。
function pathWith(prefix: string, f: Filter, since?: string): string {
  const p = filterParams(f)
  if (since) p.set('since', since)
  const s = p.toString()
  return prefix + (s ? `?${s}` : '')
}

// detailPath：过滤详情页 URL。
export const detailPath = (f: Filter, since?: string) => pathWith('/detail', f, since)

// topologyPath：流量拓扑页 URL（供拓扑与详情/主页互相跳转）。
export const topologyPath = (f: Filter, since?: string) => pathWith('/topology', f, since)

// withDim：在切片上叠加一个维度取值（点条形/维度行钻取时用）。
// 同维再叠加 = 替换（spread 覆盖旧值）；且被钉死维度的分布面板已隐藏，
// 通常不会从条形对同一维度再钻，故「同维再点」= 替换/不可达。
export function withDim(f: Filter, by: Dimension, rawKey: string): Filter {
  if (by === 'port') return { ...f, port: Number(rawKey) }
  return { ...f, [by]: rawKey } as Filter
}

// filterChips：把过滤切片展开成面包屑标签（含各约束的字段 key，供逐个删除）。
export function filterChips(f: Filter): { key: keyof Filter; label: string; value: string }[] {
  const chips: { key: keyof Filter; label: string; value: string }[] = []
  for (const d of FILTER_DIMS) {
    const v = f[d.key]
    if (v != null && v !== '') chips.push({ key: d.key, label: d.label, value: String(v) })
  }
  if (f.route) chips.push({ key: 'route', label: '类型', value: f.route === 'direct' ? '直连' : '经代理' })
  return chips
}

// withoutKey：从切片移除一个约束（面包屑 chip 删除用）。
export function withoutKey(f: Filter, key: keyof Filter): Filter {
  const next = { ...f }
  delete next[key]
  return next
}

// summary 只随过滤切片变、不含 since（KPI 概要口径：该切片的全时段总量，同主面板）。
export const getSummary = (f: Filter = EMPTY_FILTER) => getJSON<Summary>('/api/summary' + qs(f))

export const getAggregate = (by: Dimension, f: Filter = EMPTY_FILTER, since = '', limit = 20) =>
  getJSON<AggRow[]>('/api/aggregate' + qs(f, { by, since, limit }))

export const getTimeseries = (f: Filter = EMPTY_FILTER, since = '1h', bucket = '5m') =>
  getJSON<TSPoint[]>('/api/timeseries' + qs(f, { since, bucket }))

// ConnRow 是一条原始连接明细（对应后端 connections 全字段）。
export interface ConnRow {
  ts: string
  process: string
  network: string
  host: string
  port: number
  rule: string
  node: string
  region: string
}

// ConnPage 是一页明细：当前页行 + 该切片总条数。
export interface ConnPage {
  rows: ConnRow[]
  total: number
}

export const getConnections = (f: Filter = EMPTY_FILTER, since = '', offset = 0, limit = 50) =>
  getJSON<ConnPage>('/api/connections' + qs(f, { since, offset, limit }))

// 拓扑图（Sankey）数据：节点 name 带层前缀命名空间，dim+key 携真实钻取值（其它桶 key=__other__）。
export interface FlowNode {
  name: string
  dim: string
  key: string
  label: string
}
export interface FlowLink {
  source: string
  target: string
  value: number
}
export interface FlowGraph {
  nodes: FlowNode[]
  links: FlowLink[]
}

export const getFlow = (f: Filter = EMPTY_FILTER, since = '1h', limit = 10) =>
  getJSON<FlowGraph>('/api/flow' + qs(f, { since, limit }))

// 带宽度量：上行 / 下行 / 合计（up+down）。驱动 /api/traffic 的 top-N 排序。
export type Metric = 'up' | 'down' | 'total'

// TrafficRow 是某维度取值的上/下行字节合计（对应后端）。
export interface TrafficRow {
  key: string
  up: number
  down: number
}

// TrafficAgg 是 /api/traffic 的返回：按维度的字节 top-N + 该切片总上/下行。
export interface TrafficAgg {
  rows: TrafficRow[]
  totalUp: number
  totalDown: number
}

export const getTraffic = (
  by: Dimension,
  metric: Metric,
  f: Filter = EMPTY_FILTER,
  since = '',
  limit = 10,
) => getJSON<TrafficAgg>('/api/traffic' + qs(f, { by, metric, since, limit }))

// getTrafficTotals：只取某切片的总上/下行（供「流量」视图顶部展示，与维度/度量无关）。
// 各维度返回的总量一致，故取任一维度 limit=1 即可——封装在此避免调用点出现费解的任意实参。
export const getTrafficTotals = (f: Filter = EMPTY_FILTER, since = '') =>
  getJSON<TrafficAgg>('/api/traffic' + qs(f, { by: 'host', metric: 'total', since, limit: 1 }))

// trafficPath：流量视图 URL（供 Dashboard 工具栏跳转，带当前切片 + 时间窗；metric 到页内默认 total）。
export const trafficPath = (f: Filter, since?: string) => pathWith('/traffic', f, since)

// TIME_WINDOWS：流量 / 拓扑等页面共用的时间窗选项（Dashboard 另有含 bucket 的变体，故不共用那个）。
export const TIME_WINDOWS: { value: string; label: string }[] = [
  { value: '1h', label: '近 1 小时' },
  { value: '24h', label: '近 24 小时' },
  { value: '7d', label: '近 7 天' },
]
