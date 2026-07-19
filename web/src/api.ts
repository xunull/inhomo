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

// qs 把过滤切片 + 额外参数拼成查询串（只带非空项）。
function qs(f: Filter, extra: Record<string, string | number | undefined> = {}): string {
  const p = new URLSearchParams()
  if (f.host) p.set('host', f.host)
  if (f.process) p.set('process', f.process)
  if (f.node) p.set('node', f.node)
  if (f.region) p.set('region', f.region)
  if (f.port != null) p.set('port', String(f.port))
  if (f.route) p.set('route', f.route)
  for (const [k, v] of Object.entries(extra)) {
    if (v !== undefined && v !== '') p.set(k, String(v))
  }
  const s = p.toString()
  return s ? `?${s}` : ''
}

// summary 只随过滤切片变、不含 since（KPI 概要口径：该切片的全时段总量，同主面板）。
export const getSummary = (f: Filter = EMPTY_FILTER) => getJSON<Summary>('/api/summary' + qs(f))

export const getAggregate = (by: Dimension, f: Filter = EMPTY_FILTER, since = '', limit = 20) =>
  getJSON<AggRow[]>('/api/aggregate' + qs(f, { by, since, limit }))

export const getTimeseries = (f: Filter = EMPTY_FILTER, since = '1h', bucket = '5m') =>
  getJSON<TSPoint[]>('/api/timeseries' + qs(f, { since, bucket }))
