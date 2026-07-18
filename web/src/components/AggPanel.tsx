import type { ReactNode } from 'react'
import { Card, Alert, Skeleton, Empty } from 'antd'
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
  ResponsiveContainer,
} from 'recharts'
import { getAggregate } from '../api'
import { useApi } from '../useApi'

interface AggPanelProps {
  by: string // 聚合维度（host/process/node/region/port）
  title: string
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
  since,
  refreshKey,
  limit = 10,
  color = '#1677ff',
}: AggPanelProps) {
  const { data, error } = useApi(() => getAggregate(by, since, limit), [by, since, limit, refreshKey])

  let body: ReactNode
  if (error && !data) {
    body = <Alert type="error" showIcon message={error} />
  } else if (!data) {
    body = <Skeleton active paragraph={{ rows: 4 }} />
  } else if (data.length === 0) {
    body = <Empty description="暂无数据" image={Empty.PRESENTED_IMAGE_SIMPLE} />
  } else {
    // 空 key（如未识别进程）显示占位；后端已按 count 降序返回。
    const rows = data.map((r) => ({ key: r.key || '(未知)', count: r.count }))
    const height = Math.max(140, rows.length * 34)
    body = (
      <ResponsiveContainer width="100%" height={height}>
        <BarChart data={rows} layout="vertical" margin={{ left: 8, right: 24, top: 4, bottom: 4 }}>
          <CartesianGrid strokeDasharray="3 3" horizontal={false} />
          <XAxis type="number" allowDecimals={false} />
          <YAxis
            type="category"
            dataKey="key"
            width={130}
            tickFormatter={(v: string) => truncate(v)}
          />
          <Tooltip cursor={{ fill: 'rgba(0,0,0,0.04)' }} />
          <Bar dataKey="count" name="连接数" fill={color} radius={[0, 4, 4, 0]} />
        </BarChart>
      </ResponsiveContainer>
    )
  }

  return (
    <Card title={title} size="small" styles={{ body: { padding: 12 } }}>
      {body}
    </Card>
  )
}
