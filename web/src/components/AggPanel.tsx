import { Card } from 'antd'
import { useNavigate, Link } from 'react-router'
import { getAggregate, detailPath, withDim, type Dimension, type Filter } from '../api'
import { useApi } from '../useApi'
import AsyncBody from './AsyncBody'
import HBarChart from './HBarChart'

interface AggPanelProps {
  by: Dimension // 聚合维度
  title: string
  filter: Filter // 过滤切片（在此切片内做聚合）
  since: string // 时间窗（'' = 全部）；由顶层全局时间窗驱动
  refreshKey: number
  limit?: number
  color?: string
}

// AggPanel：某一维度的 top-N 条形图。传 by/标题/limit/since，内部 fetch /api/aggregate。点条形钻取。
export default function AggPanel({
  by,
  title,
  filter,
  since,
  refreshKey,
  limit = 10,
  color = '#1677ff',
}: AggPanelProps) {
  const navigate = useNavigate()
  const state = useApi(
    () => getAggregate(by, filter, since, limit),
    [by, filter, since, limit, refreshKey],
  )

  return (
    <Card
      // 面板标题 → 该维度总览页（全量排名）。
      title={
        <Link to={`/d/${by}?since=${encodeURIComponent(since)}`} style={{ color: 'inherit' }}>
          {title}
        </Link>
      }
      size="small"
      styles={{ body: { padding: 12 } }}
    >
      <AsyncBody state={state} skeletonRows={4} isEmpty={(d) => d.length === 0}>
        {(data) => {
          // 空 key（如未识别进程）显示占位；后端已按 count 降序返回。rawKey 保留原值供钻取。
          const rows = data.map((r) => ({ key: r.key || '(未知)', rawKey: r.key, count: r.count }))
          const height = Math.max(140, rows.length * 34)
          // 点条形 → 在当前切片上叠加该维度取值，跳转过滤详情页；空 key 不可钻取。
          const drill = (index: number) => {
            const raw = rows[index]?.rawKey
            if (raw) navigate(detailPath(withDim(filter, by, raw), since))
          }
          return (
            <HBarChart
              rows={rows.map((r) => ({ label: r.key, value: r.count }))}
              color={color}
              height={height}
              onBarClick={drill}
            />
          )
        }}
      </AsyncBody>
    </Card>
  )
}
