import { BrowserRouter, Routes, Route } from 'react-router'
import { Layout, Typography } from 'antd'
import { EMPTY_FILTER } from './api'
import Dashboard from './components/Dashboard'

const { Header, Content } = Layout
const { Title } = Typography

// App 是路由外壳：持久的头部 + 按路由切换的内容。
// 主页 = 全集切片的仪表盘；/detail、/d/:dim 等详情路由在后续工单加入。
export default function App() {
  return (
    <BrowserRouter>
      <Layout style={{ minHeight: '100vh' }}>
        <Header style={{ display: 'flex', alignItems: 'center' }}>
          <Title level={4} style={{ color: '#fff', margin: 0 }}>
            inhomo · 连接分析
          </Title>
        </Header>
        <Content style={{ padding: 24 }}>
          <Routes>
            <Route path="/" element={<Dashboard filter={EMPTY_FILTER} />} />
          </Routes>
        </Content>
      </Layout>
    </BrowserRouter>
  )
}
