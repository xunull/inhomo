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

export const getSummary = () => getJSON<Summary>('/api/summary')

export const getAggregate = (by: Dimension, since = '', limit = 20) =>
  getJSON<AggRow[]>(
    `/api/aggregate?by=${encodeURIComponent(by)}&since=${encodeURIComponent(since)}&limit=${limit}`,
  )

export const getTimeseries = (since = '1h', bucket = '5m') =>
  getJSON<TSPoint[]>(
    `/api/timeseries?since=${encodeURIComponent(since)}&bucket=${encodeURIComponent(bucket)}`,
  )
