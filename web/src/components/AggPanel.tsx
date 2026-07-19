import { Card } from 'antd'
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
  ResponsiveContainer,
} from 'recharts'
import { useNavigate } from 'react-router'
import { getAggregate, detailPath, withDim, type Dimension, type Filter } from '../api'
import { useApi } from '../useApi'
import AsyncBody from './AsyncBody'

interface AggPanelProps {
  by: Dimension // 聚合维度
  title: string
  filter: Filter // 过滤切片（在此切片内做聚合）
  since: string // 时间窗（'' = 全部）；由顶层全局时间窗驱动
  refreshKey: number
  limit?: number
  color?: string
}

// 截断过长的分类标签（emoji 节点名 / 长 URL），完整值仍由 Tooltip 展示，避免溢出。
function truncate(v: string, n = 12): string {
  return v.length > n ? v.slice(0, n - 1) + '…' : v
}

// AggPanel：某一维度的 top-N 条形图。传 by/标题/limit/since，内部 fetch /api/aggregate。
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
    <Card title={title} size="small" styles={{ body: { padding: 12 } }}>
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
            <ResponsiveContainer width="100%" height={height}>
              <BarChart
                data={rows}
                layout="vertical"
                margin={{ left: 8, right: 24, top: 4, bottom: 4 }}
              >
                <CartesianGrid strokeDasharray="3 3" horizontal={false} />
                <XAxis type="number" allowDecimals={false} />
                <YAxis
                  type="category"
                  dataKey="key"
                  width={130}
                  tickFormatter={(v: string) => truncate(v)}
                />
                <Tooltip cursor={{ fill: 'rgba(0,0,0,0.04)' }} />
                <Bar
                  dataKey="count"
                  name="连接数"
                  fill={color}
                  radius={[0, 4, 4, 0]}
                  cursor="pointer"
                  onClick={(_, index) => drill(index)}
                />
              </BarChart>
            </ResponsiveContainer>
          )
        }}
      </AsyncBody>
    </Card>
  )
}
