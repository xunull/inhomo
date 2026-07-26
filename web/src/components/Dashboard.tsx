import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { Row, Col, Select, Switch, Button, Space, Flex, Typography } from 'antd'
import { topologyPath, trafficPath, isFilterable, type AggDimension, type Filter } from '../api'
import KpiBar from './KpiBar'
import AggPanel from './AggPanel'
import TimeSeriesChart from './TimeSeriesChart'
import TrackerPanel from './TrackerPanel'
import ConnTable from './ConnTable'

const { Text } = Typography

type Panel = { by: AggDimension; title: string; color: string }

// 按标签长度分两组：长标签（域名 / App / 规则表达式）两列宽幅，短标签 node/region/port 三列并排。
// 「命中规则」放宽幅这组：规则形如 `DomainKeyword(github)`，挤进三列会被截断得没法看。
const TALL_PANELS: Panel[] = [
  { by: 'host', title: '热门域名', color: '#1677ff' },
  { by: 'process', title: 'App 画像', color: '#389e0d' },
  // 规则不是可过滤维度（后端 store.Filter 无 Rule 字段），故此面板不接钻取——AggPanel 按 isFilterable 自行判定。
  { by: 'rule', title: '命中规则', color: '#08979c' },
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
  // 不可过滤的维度（规则）永远不会被钉死，故恒显示。
  const shown = (p: Panel) => !isFilterable(p.by) || filter[p.by] == null
  const visibleTall = TALL_PANELS.filter(shown)
  const visibleShort = SHORT_PANELS.filter(shown)

  return (
    <>
      <Flex justify="space-between" align="center" wrap gap={12} style={{ marginBottom: 16 }}>
        <Space>
          <Text type="secondary">时间窗</Text>
          <Select value={since} onChange={setSince} options={WINDOWS} style={{ width: 130 }} />
          {/* 主页 → 全量；详情页（filter 非空）→ 当前切片。带当前时间窗。 */}
          <Link to={trafficPath(filter, since)}>流量 →</Link>
          <Link to={topologyPath(filter, since)}>流量拓扑 →</Link>
          {/* 规则缺口不接过滤切片（规则是全局配置），故不带 filter/since 参数。 */}
          <Link to="/gaps">规则缺口 →</Link>
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
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} xl={12}>
          <TrackerPanel filter={filter} since={since} refreshKey={refreshKey} />
        </Col>
      </Row>
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
