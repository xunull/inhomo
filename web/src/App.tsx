import { useEffect, useState } from 'react'
import { Layout, Typography, Row, Col, Select, Switch, Button, Space, Flex } from 'antd'
import type { Dimension } from './api'
import KpiBar from './components/KpiBar'
import AggPanel from './components/AggPanel'
import TimeSeriesChart from './components/TimeSeriesChart'

const { Header, Content } = Layout
const { Title, Text } = Typography

type Panel = { by: Dimension; title: string; color: string }

// 按基数分两组，让每行卡片高度相近、栅格填满（避免 5 个面板挤 3 列留空位、行内高矮参差）：
// 高基数维度（长列表）两列宽幅，长 host/App 名更易读。
const TALL_PANELS: Panel[] = [
  { by: 'host', title: '热门域名', color: '#1677ff' },
  { by: 'process', title: 'App 画像', color: '#52c41a' },
]
// 低基数维度（少数几项）三列并排，行内高度接近。
const SHORT_PANELS: Panel[] = [
  { by: 'node', title: '出境节点', color: '#722ed1' },
  { by: 'region', title: '地区分布', color: '#fa8c16' },
  { by: 'port', title: '目标端口', color: '#eb2f96' },
]

// 全局时间窗选项：bucket 与窗口绑定，避免时间曲线点数过多/过少（单一数据源）。
const WINDOWS = [
  { value: '1h', label: '近 1 小时', bucket: '1m' },
  { value: '24h', label: '近 24 小时', bucket: '30m' },
  { value: '7d', label: '近 7 天', bucket: '3h' },
]

const REFRESH_MS = 10_000

// 顶层集中管理时间窗与刷新：since 驱动聚合/时间曲线；refreshKey 递增触发全盘重取
// （含 summary——它不随时间窗变，只随刷新）。各子面板据此取数。
export default function App() {
  const [since, setSince] = useState('1h')
  const [auto, setAuto] = useState(true)
  const [refreshKey, setRefreshKey] = useState(0)

  useEffect(() => {
    if (!auto) return
    const id = setInterval(() => setRefreshKey((k) => k + 1), REFRESH_MS)
    return () => clearInterval(id)
  }, [auto])

  const bucket = WINDOWS.find((w) => w.value === since)?.bucket ?? '5m'

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', alignItems: 'center' }}>
        <Title level={4} style={{ color: '#fff', margin: 0 }}>
          inhomo · 连接分析
        </Title>
      </Header>
      <Content style={{ padding: 24 }}>
        <Flex justify="space-between" align="center" wrap gap={12} style={{ marginBottom: 16 }}>
          <Space>
            <Text type="secondary">时间窗</Text>
            <Select value={since} onChange={setSince} options={WINDOWS} style={{ width: 130 }} />
          </Space>
          <Space>
            <Switch checked={auto} onChange={setAuto} checkedChildren="自动" unCheckedChildren="手动" />
            <Text type="secondary">每 {REFRESH_MS / 1000}s 刷新</Text>
            <Button onClick={() => setRefreshKey((k) => k + 1)}>立即刷新</Button>
          </Space>
        </Flex>

        <KpiBar refreshKey={refreshKey} />
        <div style={{ marginTop: 16 }}>
          <TimeSeriesChart since={since} bucket={bucket} refreshKey={refreshKey} />
        </div>
        <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
          {TALL_PANELS.map((p) => (
            <Col key={p.by} xs={24} xl={12}>
              <AggPanel by={p.by} title={p.title} color={p.color} since={since} refreshKey={refreshKey} />
            </Col>
          ))}
        </Row>
        <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
          {SHORT_PANELS.map((p) => (
            <Col key={p.by} xs={24} md={12} xl={8}>
              <AggPanel by={p.by} title={p.title} color={p.color} since={since} refreshKey={refreshKey} />
            </Col>
          ))}
        </Row>
      </Content>
    </Layout>
  )
}
