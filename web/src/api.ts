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
// Dimension 是**可过滤**的维度——后端 store.Filter 有对应字段，能当钻取约束用。
export type Dimension = 'host' | 'process' | 'node' | 'region' | 'port'

// AggDimension 是**可聚合**的维度：可过滤维度 + `rule`。
// 后端 aggDimensions 白名单比可过滤维度多一个 rule（连接事件有 rule 列，但 store.Filter
// 有意没有 Rule 字段）。这里分成两个类型，是为了从**类型层面**杜绝「拿规则当过滤条件」——
// 那样后端会静默忽略该约束，用户却以为已经筛过了，比不做还糟。
export type AggDimension = Dimension | 'rule'

// isFilterable：该聚合维度能否作为过滤约束——决定面板接不接钻取。
// 同一判据也用于标题链接（维度总览页 /d/:dim 每行都要钻取，故只收可过滤维度）。
export const isFilterable = (d: AggDimension): d is Dimension => d !== 'rule'

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

export const getAggregate = (by: AggDimension, f: Filter = EMPTY_FILTER, since = '', limit = 20) =>
  getJSON<AggRow[]>('/api/aggregate' + qs(f, { by, since, limit }))

// OwnerCount 是一家追踪器归属公司在切片内的连接数。
export interface OwnerCount {
  owner: string
  count: number
}

// TrackerBreakdown 是 /api/trackers 的返回：切片内连接总数、命中已知追踪器的连接数、按归属公司 top-N。
// 归类走本机离线数据（`inhomo tracker update` 拉取）；未拉取则 tracker=0、owners 为空。
export interface TrackerBreakdown {
  total: number
  tracker: number
  owners: OwnerCount[]
  loaded: boolean // 追踪器数据是否已拉取；false → 提示跑 `inhomo tracker update`
}

export const getTrackers = (f: Filter = EMPTY_FILTER, since = '', limit = 8) =>
  getJSON<TrackerBreakdown>('/api/trackers' + qs(f, { since, limit }))

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

// FlowMetric 是拓扑边权度量：连接数（count，全量「连接事件」）或字节（up/down/total，抽样「流量记录」）。
// 是带宽度量 Metric 的超集——多一个 count。两种口径取自不同数据集（见 CONTEXT「流量拓扑」/ ADR-0008）。
export type FlowMetric = 'count' | Metric

// FLOW_METRICS：拓扑度量选项（单一事实源，驱动 Segmented 与 URL 校验）。连接数为列首 = URL 无参时的默认口径。
export const FLOW_METRICS: { value: FlowMetric; label: string }[] = [
  { value: 'count', label: '连接数' },
  { value: 'total', label: '合计' },
  { value: 'up', label: '上行' },
  { value: 'down', label: '下行' },
]

// flowMetricFromParams：URL → 拓扑度量（缺省 / 白名单外 → 连接数）。与 filterFromParams 一样，URL 是单一事实源。
export function flowMetricFromParams(p: URLSearchParams): FlowMetric {
  const m = p.get('metric')
  return FLOW_METRICS.some((x) => x.value === m) ? (m as FlowMetric) : 'count'
}

// isByteMetric：拓扑度量是否字节口径（抽样）——决定边权/tooltip 是否走 fmtBytes、是否亮出抽样提示。连接数 → false。
export const isByteMetric = (m: FlowMetric): boolean => m !== 'count'

export const getFlow = (
  f: Filter = EMPTY_FILTER,
  metric: FlowMetric = 'count',
  since = '1h',
  limit = 10,
) => getJSON<FlowGraph>('/api/flow' + qs(f, { metric, since, limit }))

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

// ExfilRow 是一条「应用通道」的外发账（见 CONTEXT「应用通道」/「外发比」/「采样覆盖率」、ADR-0013）。
// sampled/logged 是覆盖率的原始分子分母（抽样的流量记录 / 全量的连接事件），比值在 UI 侧呈现——
// 要能说清「20 条里只采到 10 条」，而不是只甩一个 50%。
export interface ExfilRow {
  process: string
  host: string
  up: number
  down: number
  ratio: number
  sampled: number
  logged: number
}

// COVERAGE_MIN 是「证据充分」的采样覆盖率下限。低于它的行照常展示、但标注为证据不足——
// 门槛（minUp/minSampled，后端默认 5MB / 10 条）挡的是「这行有没有意义」，
// 覆盖率管的是「这行可不可信」，后者披露而不隐式过滤（ADR-0013）。
export const COVERAGE_MIN = 0.5

// coverageOf：某通道的采样覆盖率；无连接事件（分母 0）时返回 null = 无从判断。
export const coverageOf = (r: ExfilRow): number | null => (r.logged > 0 ? r.sampled / r.logged : null)

// isWellEvidenced：该行的字节账是否有足够证据支撑（分母为 0 时视为不足）。
export const isWellEvidenced = (r: ExfilRow): boolean => {
  const c = coverageOf(r)
  return c !== null && c >= COVERAGE_MIN
}

// getExfil：按外发比降序的应用通道 top-N。无 by/metric 参数——主体与排序是固定的：
// 外发比不是一种排序方式，是另一种分析（故与 /api/traffic 分开，见 ADR-0013）。
export const getExfil = (f: Filter = EMPTY_FILTER, since = '', limit = 15) =>
  getJSON<ExfilRow[]>('/api/exfil' + qs(f, { since, limit }))

// NewChannel 是一条「新增应用通道」（见 CONTEXT 术语）：该 (App, host) 的全库最早连接落在窗口内。
// proxied/plaintext/tracker 是徽章，只标不滤——实测 24h 内「明文 + 经出境节点」的新增仅 2 条，
// 拿它当过滤条件会得到一个空页面（ADR-0014）。
export interface NewChannel {
  process: string
  host: string
  firstTs: string
  count: number
  proxied: boolean
  plaintext: boolean
  tracker: string // 追踪器归属公司；空 = 未命中或未拉取追踪器数据
}

// NewAppGroup 是按 App 归组的新增。muted = 「用户指定目的地的进程」（浏览器类），
// 其新增是用户自己行为的产物，默认折叠——但**保留**，随时可展开。
export interface NewAppGroup {
  process: string
  count: number
  muted: boolean
  channels: NewChannel[]
}

// CoverageGap 是一段没在记录的空洞。
export interface CoverageGap {
  start: string
  end: string
  hours: number
}

// Coverage 是「观测覆盖」（见 CONTEXT 术语）：库中确实在记录的时间范围与空洞。
// 它是新鲜度结论的前提——中断期间出现过的通道，恢复记录后会被误报为「新」。
export interface Coverage {
  earliest: string | null
  latest: string | null
  coveredHours: number
  gaps: CoverageGap[]
}

export interface NewChannelsResp {
  apps: NewAppGroup[]
  coverage: Coverage
  truncated: boolean
}

// NEW_WINDOWS：新鲜度视图自带的时间窗，**与主页的不同**。
// 实测新增量：1h→14 条 / 24h→254 / 7d→2745，跨两个数量级，故不跟随主页 1h 档（ADR-0014）。
export const NEW_WINDOWS: { value: string; label: string }[] = [
  { value: '24h', label: '近 24 小时' },
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' },
]

// getNewChannels：不带过滤切片——首次出现要拿窗口外的历史当参照系，不是切片内可算的约束
// （见 CONTEXT「过滤切片」的边界说明）。
export const getNewChannels = (since = '24h', limit = 0) =>
  getJSON<NewChannelsResp>('/api/new' + qs(EMPTY_FILTER, { since, limit: limit || undefined }))

// getNewCount：只取一个数字，供主页 KPI 当 /new 的入口钩子。
// 主页每 10s 自动刷新，不该为一个数字把整份新增清单（24h 约 250 条）拉过来。
// 口径与 /new 页面上「未折叠」的部分一致（后端同样排除了 mute 名单）。
export const getNewCount = (since = '24h') =>
  getJSON<{ count: number }>('/api/new/count' + qs(EMPTY_FILTER, { since }))

// FallthroughHost 是一个兜底目的 host 的汇总（bytes 来自抽样的流量记录、按 host 关联）。
export interface FallthroughHost {
  host: string
  conns: number
  bytes: number
  lastTs: string
}

// RuleGap 是一条「规则缺口」（见 CONTEXT 术语）：一个可注册域 + 它的兜底汇总，
// 也就是**一条待补的分流规则**。hosts 是其下的具体子域，供展开核实覆盖面。
export interface RuleGap {
  domain: string
  conns: number
  bytes: number
  lastTs: string
  hosts: FallthroughHost[]
}

// GapsResp：totalConns/totalBytes 的分母**含 IP 目标区**，所以域名清单的累计覆盖
// 到底也不会满 100%——差额正是 IP 区那部分。
export interface GapsResp {
  gaps: RuleGap[]
  ipTargets: FallthroughHost[]
  bypassed: number // GLOBAL / specialProxy：未经规则匹配，补规则对其无效
  totalConns: number
  totalBytes: number
}

// GAP_WINDOWS：规则缺口视图自带的时间窗。默认 7 天而非全部——
// 早已补过规则的域名会一直留在历史里，全时段会把清单变成「历史上所有缺口的并集」。
export const GAP_WINDOWS: { value: string; label: string }[] = [
  { value: '24h', label: '近 24 小时' },
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' },
  { value: '', label: '全部时间' },
]

// getGaps：不带过滤切片——规则是全局配置，不分切片。
export const getGaps = (since = '7d') => getJSON<GapsResp>('/api/gaps' + qs(EMPTY_FILTER, { since }))

// ruleSnippet：把一条缺口/目标转成可粘贴的规则片段。
// 策略组名 inhomo 拿不到（detect.go 的 effectiveNode 在解析时就把分组名剥掉了，
// 见 ADR-0015），故留占位符由用户自行替换。
export const RULE_GROUP_PLACEHOLDER = '<策略组>'
export const domainRule = (domain: string) => `  - DOMAIN-SUFFIX,${domain},${RULE_GROUP_PLACEHOLDER}`
// IP 目标写不了域名规则；v1 只给单地址形态（/32、/128），不推断掩码。
export const ipRule = (host: string) => {
  const bare = host.replace(/^\[|\]$/g, '')
  const isV6 = bare.includes(':')
  return `  - IP-CIDR${isV6 ? '6' : ''},${bare}/${isV6 ? 128 : 32},${RULE_GROUP_PLACEHOLDER},no-resolve`
}

// TIME_WINDOWS：流量 / 拓扑等页面共用的时间窗选项（Dashboard 另有含 bucket 的变体，故不共用那个）。
export const TIME_WINDOWS: { value: string; label: string }[] = [
  { value: '1h', label: '近 1 小时' },
  { value: '24h', label: '近 24 小时' },
  { value: '7d', label: '近 7 天' },
]
