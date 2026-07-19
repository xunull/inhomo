import { useEffect, useState } from 'react'
import { Layout, Typography, Row, Col, Select, Switch, Button, Space, Flex } from 'antd'
import KpiBar from './components/KpiBar'
import AggPanel from './components/AggPanel'
import TimeSeriesChart from './components/TimeSeriesChart'

const { Header, Content } = Layout
const { Title, Text } = Typography

// 5 个聚合维度面板。
const PANELS = [
  { by: 'host', title: '热门域名', color: '#1677ff' },
  { by: 'process', title: 'App 画像', color: '#52c41a' },
  { by: 'node', title: '出境节点', color: '#722ed1' },
  { by: 'region', title: '地区分布', color: '#fa8c16' },
  { by: 'port', title: '目标端口', color: '#eb2f96' },
]

// 全局时间窗选项。
const WINDOWS = [
  { value: '1h', label: '近 1 小时' },
  { value: '24h', label: '近 24 小时' },
  { value: '7d', label: '近 7 天' },
]

const REFRESH_MS = 10_000

// bucketFor：时间曲线的桶粒度随窗口自动取合理值，避免点数过多/过少。
function bucketFor(since: string): string {
  switch (since) {
    case '1h':
      return '1m'
    case '24h':
      return '30m'
    case '7d':
      return '3h'
    default:
      return '5m'
  }
}

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

  const bucket = bucketFor(since)

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
          {PANELS.map((p) => (
            <Col key={p.by} xs={24} md={12} xl={8}>
              <AggPanel
                by={p.by}
                title={p.title}
                color={p.color}
                since={since}
                refreshKey={refreshKey}
              />
            </Col>
          ))}
        </Row>
      </Content>
    </Layout>
  )
}
