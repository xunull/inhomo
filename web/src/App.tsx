import { lazy, Suspense } from 'react'
import { BrowserRouter, Routes, Route, Link } from 'react-router'
import { Layout, Typography, Skeleton } from 'antd'
import { EMPTY_FILTER } from './api'
import Dashboard from './components/Dashboard'
import DetailPage from './components/DetailPage'
import DimensionOverview from './components/DimensionOverview'
import TrafficPage from './components/TrafficPage'
import NewPage from './components/NewPage'
import GapsPage from './components/GapsPage'
import HeaderNav from './components/HeaderNav'

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
        {/* Header 同时承载「站点身份」与「顶级导航」——这是最显眼、且每页都在的持久表面，
            此前只放了一句标题。站点名固定在左（点击回主页），导航紧随其后。 */}
        <Header style={{ display: 'flex', alignItems: 'center', gap: 24, paddingInline: 24 }}>
          <Title level={4} style={{ color: '#fff', margin: 0, whiteSpace: 'nowrap', flex: 'none' }}>
            <Link to="/" style={{ color: '#fff' }}>
              inhomo
            </Link>
          </Title>
          <HeaderNav />
        </Header>
        <Content style={{ padding: 24 }}>
          <Routes>
            <Route path="/" element={<Dashboard filter={EMPTY_FILTER} />} />
            <Route path="/detail" element={<DetailPage />} />
            <Route path="/d/:dim" element={<DimensionOverview />} />
            <Route path="/traffic" element={<TrafficPage />} />
            {/* /new 自带时间窗，不接过滤切片——首次出现要拿窗口外历史当参照系。 */}
            <Route path="/new" element={<NewPage />} />
            {/* /gaps 同样不接切片——规则是全局配置，不分切片。 */}
            <Route path="/gaps" element={<GapsPage />} />
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
