import { useMemo, useState } from 'react'
import { useSearchParams, Link } from 'react-router'
import { Card, Select, Space, Tag, Typography } from 'antd'
import { getFlow, filterFromParams, filterChips } from '../api'
import { useApi } from '../useApi'
import AsyncBody from './AsyncBody'
import TopologyChart from './TopologyChart'

const { Text } = Typography

// 本页的时间窗（T26 会并入 Dashboard 工具栏；此处先自带一个最小选择器）。
const WINDOWS = [
  { value: '1h', label: '近 1 小时' },
  { value: '24h', label: '近 24 小时' },
  { value: '7d', label: '近 7 天' },
]

// TopologyPage：/topology 路由。App→出境节点 的 ECharts Sankey，受 URL 过滤切片 + 时间窗驱动。
export default function TopologyPage() {
  const [params] = useSearchParams()
  const paramKey = params.toString()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const filter = useMemo(() => filterFromParams(params), [paramKey])
  const [since, setSince] = useState(params.get('since') || '1h')

  const state = useApi(() => getFlow(filter, since, 12), [filter, since])
  const chips = filterChips(filter)

  return (
    <>
      <Space wrap align="center" style={{ marginBottom: 16 }}>
        <Link to="/">← 仪表盘</Link>
        <Text type="secondary">/ 流量拓扑</Text>
        {chips.map((c) => (
          <Tag key={c.key} color="blue">
            {c.label}：{c.value}
          </Tag>
        ))}
        <Text type="secondary">时间窗</Text>
        <Select value={since} onChange={setSince} options={WINDOWS} style={{ width: 120 }} />
      </Space>
      <Card size="small" title="App → 出境节点 流量拓扑">
        <AsyncBody
          state={state}
          skeletonRows={8}
          isEmpty={(d) => d.links.length === 0}
          emptyText="该时间窗/切片内暂无连接"
        >
          {(data) => <TopologyChart graph={data} />}
        </AsyncBody>
      </Card>
    </>
  )
}
