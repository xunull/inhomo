import { useEffect, useState } from 'react'
import { Layout, Typography, Spin, Alert } from 'antd'
import { getSummary, type Summary } from './api'

const { Header, Content } = Layout
const { Title, Text } = Typography

// T14 骨架：验证 antd 渲染 + API client 连通后端。真实面板/图在 T15–T18 加。
export default function App() {
  const [summary, setSummary] = useState<Summary | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    getSummary()
      .then(setSummary)
      .catch((e: unknown) => setErr(e instanceof Error ? e.message : String(e)))
  }, [])

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', alignItems: 'center' }}>
        <Title level={4} style={{ color: '#fff', margin: 0 }}>
          inhomo · 连接分析
        </Title>
      </Header>
      <Content style={{ padding: 24 }}>
        <Title level={5}>仪表盘（骨架）</Title>
        {err && <Alert type="error" showIcon message={`后端未连通：${err}`} />}
        {!err && !summary && <Spin />}
        {summary && (
          <Text>
            后端连通 ✔ 当前已记录 <b>{summary.total}</b> 条连接
          </Text>
        )}
      </Content>
    </Layout>
  )
}
