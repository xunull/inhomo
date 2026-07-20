import { useMemo, useState } from 'react'
import { useSearchParams, Link } from 'react-router'
import { Button, Card, Col, Row, Segmented, Select, Space, Statistic, Tag, Typography } from 'antd'
import {
  getTrafficTotals,
  filterFromParams,
  filterChips,
  TIME_WINDOWS,
  type Dimension,
  type Metric,
} from '../api'
import { useApi } from '../useApi'
import { fmtBytes } from '../format'
import AsyncBody from './AsyncBody'
import TrafficPanel from './TrafficPanel'

const { Text } = Typography

// 度量选项：合计默认。
const METRICS: { value: Metric; label: string }[] = [
  { value: 'total', label: '合计' },
  { value: 'up', label: '上行' },
  { value: 'down', label: '下行' },
]

// 按维度的 top-N 面板（host/App/节点）。
const PANELS: { by: Dimension; title: string; color: string }[] = [
  { by: 'host', title: '域名 · 带宽 top', color: '#1677ff' },
  { by: 'process', title: 'App · 带宽 top', color: '#389e0d' },
  { by: 'node', title: '出境节点 · 带宽 top', color: '#722ed1' },
]

const isMetric = (v: string | null): v is Metric => v === 'up' || v === 'down' || v === 'total'

// TrafficPage：/traffic 路由。顶部切片总上/下行 + 按维度的字节 top-N，受 URL 过滤切片 + 时间窗 + 度量驱动。
export default function TrafficPage() {
  const [params, setParams] = useSearchParams()
  const [refreshKey, setRefreshKey] = useState(0)
  const paramKey = params.toString()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const filter = useMemo(() => filterFromParams(params), [paramKey])
  // since / metric 以 URL 为单一事实源（可分享、与详情页一致）。
  const since = params.get('since') || '1h'
  const metricParam = params.get('metric')
  const metric: Metric = isMetric(metricParam) ? metricParam : 'total'
  const setParam = (k: string, v: string) => {
    const next = new URLSearchParams(params)
    next.set(k, v)
    setParams(next, { replace: true })
  }

  // 顶部总量：一次专门取数（与维度/度量无关，见 getTrafficTotals）。
  const totals = useApi(() => getTrafficTotals(filter, since), [filter, since, refreshKey])
  const chips = filterChips(filter)

  return (
    <>
      <Space wrap align="center" style={{ marginBottom: 16 }}>
        <Link to="/">← 仪表盘</Link>
        <Text type="secondary">/ 流量</Text>
        {chips.map((c) => (
          <Tag key={c.key} color="blue">
            {c.label}：{c.value}
          </Tag>
        ))}
        <Text type="secondary">时间窗</Text>
        <Select value={since} onChange={(v) => setParam('since', v)} options={TIME_WINDOWS} style={{ width: 120 }} />
        <Text type="secondary">度量</Text>
        <Segmented value={metric} onChange={(v) => setParam('metric', String(v))} options={METRICS} />
        <Button onClick={() => setRefreshKey((k) => k + 1)}>立即刷新</Button>
      </Space>

      <Card size="small" style={{ marginBottom: 16 }}>
        <AsyncBody state={totals} skeletonRows={1}>
          {(d) => (
            <Row gutter={16}>
              <Col xs={12} md={8}>
                <Statistic title="总上行 ↑" value={fmtBytes(d.totalUp)} valueStyle={{ color: '#389e0d' }} />
              </Col>
              <Col xs={12} md={8}>
                <Statistic title="总下行 ↓" value={fmtBytes(d.totalDown)} valueStyle={{ color: '#1677ff' }} />
              </Col>
              <Col xs={24} md={8}>
                <Statistic title="总计" value={fmtBytes(d.totalUp + d.totalDown)} />
              </Col>
            </Row>
          )}
        </AsyncBody>
      </Card>

      <Row gutter={[16, 16]}>
        {/* 隐藏被精确过滤钉死的维度面板（只剩一个值的 top-N 没意义）。 */}
        {PANELS.filter((p) => filter[p.by] == null).map((p) => (
          <Col key={p.by} xs={24} xl={8}>
            <TrafficPanel
              by={p.by}
              title={p.title}
              color={p.color}
              metric={metric}
              filter={filter}
              since={since}
              refreshKey={refreshKey}
            />
          </Col>
        ))}
      </Row>
    </>
  )
}
