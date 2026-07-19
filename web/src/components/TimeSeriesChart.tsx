import { Card } from 'antd'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
  ResponsiveContainer,
} from 'recharts'
import { getTimeseries } from '../api'
import { useApi } from '../useApi'
import { fmtDateTime, fmtTimeShort } from '../format'
import AsyncBody from './AsyncBody'

interface Props {
  since: string // 时间窗
  bucket: string // 桶粒度
  refreshKey: number
}

// TimeSeriesChart：连接数随时间的面积图，数据 [{ts,count}] 已按时间升序。
export default function TimeSeriesChart({ since, bucket, refreshKey }: Props) {
  const state = useApi(() => getTimeseries(since, bucket), [since, bucket, refreshKey])

  return (
    <Card title="连接数时间曲线" size="small">
      <AsyncBody
        state={state}
        skeletonRows={5}
        isEmpty={(d) => d.length === 0}
        emptyText="该时间窗内暂无连接"
      >
        {(data) => (
          <ResponsiveContainer width="100%" height={280}>
            <AreaChart data={data} margin={{ left: 8, right: 16, top: 8, bottom: 4 }}>
              <defs>
                <linearGradient id="tsFill" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="#1677ff" stopOpacity={0.35} />
                  <stop offset="95%" stopColor="#1677ff" stopOpacity={0.02} />
                </linearGradient>
              </defs>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="ts" tickFormatter={fmtTimeShort} minTickGap={32} />
              <YAxis allowDecimals={false} />
              <Tooltip labelFormatter={(l) => fmtDateTime(l as string)} />
              <Area
                type="monotone"
                dataKey="count"
                name="连接数"
                stroke="#1677ff"
                strokeWidth={2}
                fill="url(#tsFill)"
              />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </AsyncBody>
    </Card>
  )
}
