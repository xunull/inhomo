import { Menu } from 'antd'
import { useLocation, useNavigate } from 'react-router'

// NAV_ITEMS 是全部**顶级分析视图**（单一事实源）。顺序即叙事：
// 全景概览 → 字节视角 → 流向 → 新鲜度 → 待办。
//
// 这里只放顶级视图。`/detail`（过滤切片详情）与 `/d/:dim`（维度总览）是从数据钻取
// 进去的，不是并列的目的地，故不进导航——它们各自保留自己的返回条。
const NAV_ITEMS = [
  { key: '/', label: '仪表盘' },
  { key: '/traffic', label: '流量' },
  { key: '/topology', label: '流量拓扑' },
  { key: '/new', label: '新增' },
  { key: '/gaps', label: '规则缺口' },
]

// selectedKeyFor：当前路径 → 该高亮哪一项。
// 钻取页（/detail、/d/:dim）算作「仪表盘」的下级，高亮仪表盘——用户是从那儿下来的，
// 高亮它比什么都不亮更能回答「我在哪」。
function selectedKeyFor(pathname: string): string {
  const hit = NAV_ITEMS.find((i) => i.key !== '/' && pathname.startsWith(i.key))
  return hit ? hit.key : '/'
}

// HeaderNav：全站持久的顶级导航，放在原本只装一句标题的深色 Header 里。
//
// 它一次解决三件事：每一页都答得出「我在哪、一共有哪几页」（trunk test）；
// 点击区从 17px 的裸文字变成 Menu 的标准高度；5 个顶级视图从三套入口机制
// （工具栏文字链 / KPI 卡片 / 无）收敛成一套。
export default function HeaderNav() {
  const { pathname } = useLocation()
  const navigate = useNavigate()

  return (
    <Menu
      theme="dark"
      mode="horizontal"
      selectedKeys={[selectedKeyFor(pathname)]}
      items={NAV_ITEMS}
      onClick={({ key }) => navigate(key)}
      // 撑满剩余宽度：窄屏时 antd 会自动把放不下的项收进溢出菜单，不会挤成两行。
      style={{ flex: 1, minWidth: 0, borderBottom: 'none', background: 'transparent' }}
    />
  )
}
