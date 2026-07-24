import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
  ResponsiveContainer,
} from 'recharts'
import { truncate } from '../format'

interface HBarChartProps {
  rows: { label: string; value: number }[] // label = 类目轴、value = 数值轴（连接数）
  color: string
  height: number
  onBarClick?: (index: number) => void // 传则条形可点（钻取）；省略则纯展示、无指针
}

// HBarChart：横向 top-N 条形图。AggPanel（可钻取）与 TrackerPanel（不可钻）共用，消除两处近乎逐字的复制。
export default function HBarChart({ rows, color, height, onBarClick }: HBarChartProps) {
  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={rows} layout="vertical" margin={{ left: 8, right: 24, top: 4, bottom: 4 }}>
        <CartesianGrid strokeDasharray="3 3" horizontal={false} />
        <XAxis type="number" allowDecimals={false} />
        <YAxis
          type="category"
          dataKey="label"
          width={130}
          tickFormatter={(v: string) => truncate(v)}
        />
        <Tooltip cursor={{ fill: 'rgba(0,0,0,0.04)' }} />
        <Bar
          dataKey="value"
          name="连接数"
          fill={color}
          radius={[0, 4, 4, 0]}
          cursor={onBarClick ? 'pointer' : undefined}
          onClick={onBarClick ? (_, index) => onBarClick(index) : undefined}
        />
      </BarChart>
    </ResponsiveContainer>
  )
}
