import { useState } from 'react'
import { Layout, Typography } from 'antd'
import KpiBar from './components/KpiBar'

const { Header, Content } = Layout
const { Title } = Typography

export default function App() {
  // refreshKey 递增即触发全盘重取；T18 会由自动刷新驱动它，此前恒为 0。
  const [refreshKey] = useState(0)

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', alignItems: 'center' }}>
        <Title level={4} style={{ color: '#fff', margin: 0 }}>
          inhomo · 连接分析
        </Title>
      </Header>
      <Content style={{ padding: 24 }}>
        <KpiBar refreshKey={refreshKey} />
      </Content>
    </Layout>
  )
}
