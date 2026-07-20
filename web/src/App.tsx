import { lazy, Suspense } from 'react'
import { BrowserRouter, Routes, Route, Link } from 'react-router'
import { Layout, Typography, Skeleton } from 'antd'
import { EMPTY_FILTER } from './api'
import Dashboard from './components/Dashboard'
import DetailPage from './components/DetailPage'
import DimensionOverview from './components/DimensionOverview'
import TrafficPage from './components/TrafficPage'

// 拓扑页懒加载：echarts 随它单独成 chunk，不进主仪表盘首屏包。
const TopologyPage = lazy(() => import('./components/TopologyPage'))

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
            <Route path="/traffic" element={<TrafficPage />} />
            <Route
              path="/topology"
              element={
                <Suspense fallback={<Skeleton active paragraph={{ rows: 8 }} />}>
                  <TopologyPage />
                </Suspense>
              }
            />
          </Routes>
        </Content>
      </Layout>
    </BrowserRouter>
  )
}
