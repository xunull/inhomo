import { BrowserRouter, Routes, Route, Link } from 'react-router'
import { Layout, Typography } from 'antd'
import { EMPTY_FILTER } from './api'
import Dashboard from './components/Dashboard'
import DetailPage from './components/DetailPage'
import DimensionOverview from './components/DimensionOverview'

const { Header, Content } = Layout
const { Title } = Typography

// App 是路由外壳：持久的头部 + 按路由切换的内容。
// 主页 = 全集切片的仪表盘；/detail = 过滤切片详情；/d/:dim = 维度全量排名总览。
export default function App() {
  return (
    <BrowserRouter>
      <Layout style={{ minHeight: '100vh' }}>
        <Header style={{ display: 'flex', alignItems: 'center' }}>
          <Title level={4} style={{ color: '#fff', margin: 0 }}>
            <Link to="/" style={{ color: '#fff' }}>
              inhomo · 连接分析
            </Link>
          </Title>
        </Header>
        <Content style={{ padding: 24 }}>
          <Routes>
            <Route path="/" element={<Dashboard filter={EMPTY_FILTER} />} />
            <Route path="/detail" element={<DetailPage />} />
            <Route path="/d/:dim" element={<DimensionOverview />} />
          </Routes>
        </Content>
      </Layout>
    </BrowserRouter>
  )
}
