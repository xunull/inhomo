import { useEffect, useState } from 'react'
import { Row, Col, Select, Switch, Button, Space, Flex, Typography } from 'antd'
import type { Dimension, Filter } from '../api'
import KpiBar from './KpiBar'
import AggPanel from './AggPanel'
import TimeSeriesChart from './TimeSeriesChart'
import ConnTable from './ConnTable'

const { Text } = Typography

type Panel = { by: Dimension; title: string; color: string }

// 按基数分两组：高基数 host/App 两列宽幅，低基数 node/region/port 三列并排。
const TALL_PANELS: Panel[] = [
  { by: 'host', title: '热门域名', color: '#1677ff' },
  { by: 'process', title: 'App 画像', color: '#389e0d' },
]
const SHORT_PANELS: Panel[] = [
  { by: 'node', title: '出境节点', color: '#722ed1' },
  { by: 'region', title: '地区分布', color: '#d46b08' },
  { by: 'port', title: '目标端口', color: '#c41d7f' },
]

// 全局时间窗选项：bucket 与窗口绑定（单一数据源）。
const WINDOWS = [
  { value: '1h', label: '近 1 小时', bucket: '1m' },
  { value: '24h', label: '近 24 小时', bucket: '30m' },
  { value: '7d', label: '近 7 天', bucket: '3h' },
]

const REFRESH_MS = 10_000

interface DashboardProps {
  filter: Filter
  initialSince?: string // 详情页从 URL 继承的时间窗
  initialAuto?: boolean // 详情页默认关自动刷新
  showConnections?: boolean // 详情页在面板下方展示原始明细表
}

// Dashboard：一个「过滤切片」的分析视图。主页传空切片；详情页传带约束的切片 + 明细表。
// 自身管理时间窗 / 自动刷新 / refreshKey，把 filter+since+refreshKey 透传给各子面板与明细表。
export default function Dashboard({
  filter,
  initialSince = '1h',
  initialAuto = true,
  showConnections = false,
}: DashboardProps) {
  const [since, setSince] = useState(initialSince)
  const [auto, setAuto] = useState(initialAuto)
  const [refreshKey, setRefreshKey] = useState(0)

  useEffect(() => {
    if (!auto) return
    const id = setInterval(() => setRefreshKey((k) => k + 1), REFRESH_MS)
    return () => clearInterval(id)
  }, [auto])

  const bucket = WINDOWS.find((w) => w.value === since)?.bucket ?? '5m'
  // 隐藏被精确过滤钉死的维度面板（只剩一个值的分布没意义）。
  const visibleTall = TALL_PANELS.filter((p) => filter[p.by] == null)
  const visibleShort = SHORT_PANELS.filter((p) => filter[p.by] == null)

  return (
    <>
      <Flex justify="space-between" align="center" wrap gap={12} style={{ marginBottom: 16 }}>
        <Space>
          <Text type="secondary">时间窗</Text>
          <Select value={since} onChange={setSince} options={WINDOWS} style={{ width: 130 }} />
        </Space>
        <Space>
          <Switch checked={auto} onChange={setAuto} checkedChildren="自动" unCheckedChildren="手动" />
          <Text type="secondary">{auto ? `每 ${REFRESH_MS / 1000}s 刷新` : '自动刷新已暂停'}</Text>
          <Button onClick={() => setRefreshKey((k) => k + 1)}>立即刷新</Button>
        </Space>
      </Flex>

      <KpiBar filter={filter} since={since} refreshKey={refreshKey} />
      <div style={{ marginTop: 16 }}>
        <TimeSeriesChart filter={filter} since={since} bucket={bucket} refreshKey={refreshKey} />
      </div>
      {visibleTall.length > 0 && (
        <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
          {visibleTall.map((p) => (
            <Col key={p.by} xs={24} xl={12}>
              <AggPanel by={p.by} title={p.title} color={p.color} filter={filter} since={since} refreshKey={refreshKey} />
            </Col>
          ))}
        </Row>
      )}
      {visibleShort.length > 0 && (
        <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
          {visibleShort.map((p) => (
            <Col key={p.by} xs={24} md={12} xl={8}>
              <AggPanel by={p.by} title={p.title} color={p.color} filter={filter} since={since} refreshKey={refreshKey} />
            </Col>
          ))}
        </Row>
      )}
      {showConnections && (
        <div style={{ marginTop: 16 }}>
          {/* key 随切片/时间窗变化 → 重挂载明细表，分页复位到第一页（避免旧页码落到新切片空页）。 */}
          <ConnTable
            key={`${JSON.stringify(filter)}|${since}`}
            filter={filter}
            since={since}
            refreshKey={refreshKey}
          />
        </div>
      )}
    </>
  )
}
