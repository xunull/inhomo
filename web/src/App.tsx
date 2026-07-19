import { useState } from 'react'
import { Layout, Typography, Row, Col } from 'antd'
import KpiBar from './components/KpiBar'
import AggPanel from './components/AggPanel'
import TimeSeriesChart from './components/TimeSeriesChart'

const { Header, Content } = Layout
const { Title } = Typography

// 5 个聚合维度面板。
const PANELS = [
  { by: 'host', title: '热门域名', color: '#1677ff' },
  { by: 'process', title: 'App 画像', color: '#52c41a' },
  { by: 'node', title: '出境节点', color: '#722ed1' },
  { by: 'region', title: '地区分布', color: '#fa8c16' },
  { by: 'port', title: '目标端口', color: '#eb2f96' },
]

export default function App() {
  // refreshKey 递增即触发全盘重取；T18 会由自动刷新驱动它，此前恒为 0。
  const [refreshKey] = useState(0)
  // since 时间窗（'' = 全部）；T18 会由全局时间窗 Select 驱动，此前恒为全部。
  const since = ''

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', alignItems: 'center' }}>
        <Title level={4} style={{ color: '#fff', margin: 0 }}>
          inhomo · 连接分析
        </Title>
      </Header>
      <Content style={{ padding: 24 }}>
        <KpiBar refreshKey={refreshKey} />
        <div style={{ marginTop: 16 }}>
          {/* 时间曲线默认近 1h / 桶 5m；T18 会由全局时间窗驱动 since 与自动推导的 bucket。 */}
          <TimeSeriesChart since="1h" bucket="5m" refreshKey={refreshKey} />
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
