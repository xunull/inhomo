import { useMemo, useState } from 'react'
import { useSearchParams, useNavigate, Link } from 'react-router'
import { Button, Card, Segmented, Select, Space, Tag, Typography } from 'antd'
import {
  getFlow,
  detailPath,
  filterFromParams,
  filterChips,
  withDim,
  flowMetricFromParams,
  isByteMetric,
  FLOW_METRICS,
  TIME_WINDOWS,
  type Dimension,
} from '../api'
import { useApi } from '../useApi'
import AsyncBody from './AsyncBody'
import TopologyChart from './TopologyChart'

const { Text } = Typography

// TopologyPage：/topology 路由。App→出境节点 的 ECharts Sankey，受 URL 过滤切片 + 时间窗驱动。
export default function TopologyPage() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const [refreshKey, setRefreshKey] = useState(0)
  const paramKey = params.toString()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const filter = useMemo(() => filterFromParams(params), [paramKey])
  // since / metric 以 URL 为单一事实源（可分享、刷新、前进后退都保持）；改控件即写回 URL。
  const since = params.get('since') || '1h'
  const metric = flowMetricFromParams(params)
  const byteMetric = isByteMetric(metric)
  const setParam = (k: string, v: string) => {
    const next = new URLSearchParams(params)
    next.set(k, v)
    setParams(next, { replace: true })
  }

  const state = useApi(() => getFlow(filter, metric, since, 12), [filter, metric, since, refreshKey])
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
        <Select value={since} onChange={(v) => setParam('since', v)} options={TIME_WINDOWS} style={{ width: 120 }} />
        <Text type="secondary">度量</Text>
        <Segmented value={metric} onChange={(v) => setParam('metric', String(v))} options={FLOW_METRICS} />
        {byteMetric && (
          <Text type="warning" style={{ fontSize: 12 }}>
            字节口径为抽样，短连接可能漏计
          </Text>
        )}
        <Button onClick={() => setRefreshKey((k) => k + 1)}>立即刷新</Button>
      </Space>
      <Card size="small" title="App → 出境节点 流量拓扑（点节点钻取详情）">
        <AsyncBody
          state={state}
          skeletonRows={8}
          isEmpty={(d) => d.links.length === 0}
          emptyText="该时间窗/切片内暂无连接"
        >
          {(data) => (
            <TopologyChart
              graph={data}
              byteMetric={byteMetric}
              onNodeClick={(dim, key) =>
                navigate(detailPath(withDim(filter, dim as Dimension, key), since))
              }
            />
          )}
        </AsyncBody>
      </Card>
    </>
  )
}
