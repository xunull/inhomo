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
import {
  getTraffic,
  detailPath,
  withDim,
  type Dimension,
  type Filter,
  type Metric,
  type TrafficRow,
} from '../api'
import { useApi } from '../useApi'
import { fmtBytes, truncate } from '../format'
import AsyncBody from './AsyncBody'

interface TrafficPanelProps {
  by: Dimension // 聚合维度（host/process/node/…）
  title: string
  metric: Metric // 上行/下行/合计：驱动条形取值与排序
  filter: Filter // 过滤切片
  since: string // 时间窗（'' = 全部）
  refreshKey: number
  limit?: number
  color?: string
}

// metricValue：按当前度量取一行的字节值（合计 = 上+下）。
function metricValue(r: TrafficRow, metric: Metric): number {
  if (metric === 'up') return r.up
  if (metric === 'down') return r.down
  return r.up + r.down
}

type Row = { key: string; rawKey: string; up: number; down: number; value: number }

// 自定义 Tooltip：无论当前度量，都展示该项的上行/下行明细（回答「上传多少下载多少」）。
function BytesTooltip({ active, payload }: { active?: boolean; payload?: { payload: Row }[] }) {
  if (!active || !payload?.length) return null
  const r = payload[0].payload
  return (
    <div
      style={{
        background: '#fff',
        border: '1px solid #f0f0f0',
        borderRadius: 6,
        padding: '6px 10px',
        boxShadow: '0 2px 8px rgba(0,0,0,0.12)',
        fontSize: 12,
      }}
    >
      <div style={{ fontWeight: 600, marginBottom: 4, wordBreak: 'break-all', maxWidth: 260 }}>
        {r.key}
      </div>
      <div>↑ 上行：{fmtBytes(r.up)}</div>
      <div>↓ 下行：{fmtBytes(r.down)}</div>
    </div>
  )
}

// TrafficPanel：某维度按字节的 top-N 条形。内部 fetch /api/traffic；点条形 → 该取值的过滤详情。
export default function TrafficPanel({
  by,
  title,
  metric,
  filter,
  since,
  refreshKey,
  limit = 10,
  color = '#1677ff',
}: TrafficPanelProps) {
  const navigate = useNavigate()
  const state = useApi(
    () => getTraffic(by, metric, filter, since, limit),
    [by, metric, filter, since, limit, refreshKey],
  )

  return (
    <Card title={title} size="small" styles={{ body: { padding: 12 } }}>
      <AsyncBody
        state={state}
        skeletonRows={4}
        isEmpty={(d) => d.rows.length === 0}
        emptyText="该时间窗/切片内暂无流量"
      >
        {(data) => {
          const rows: Row[] = data.rows.map((r) => ({
            key: r.key || '(未知)',
            rawKey: r.key,
            up: r.up,
            down: r.down,
            value: metricValue(r, metric),
          }))
          const height = Math.max(140, rows.length * 34)
          // 点条形 → 在当前切片叠加该维度取值，跳过滤详情页；空 key 不可钻取。
          const drill = (index: number) => {
            const raw = rows[index]?.rawKey
            if (raw) navigate(detailPath(withDim(filter, by, raw), since))
          }
          return (
            <ResponsiveContainer width="100%" height={height}>
              <BarChart data={rows} layout="vertical" margin={{ left: 8, right: 24, top: 4, bottom: 4 }}>
                <CartesianGrid strokeDasharray="3 3" horizontal={false} />
                <XAxis type="number" tickFormatter={(v: number) => fmtBytes(v)} />
                <YAxis
                  type="category"
                  dataKey="key"
                  width={130}
                  tickFormatter={(v: string) => truncate(v)}
                />
                <Tooltip cursor={{ fill: 'rgba(0,0,0,0.04)' }} content={<BytesTooltip />} />
                <Bar
                  dataKey="value"
                  fill={color}
                  radius={[0, 4, 4, 0]}
                  cursor="pointer"
                  // 关入场动画：切换度量（合计/上行/下行）时条形不逐次从 0 重播，切换即时且不卡顿。
                  isAnimationActive={false}
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
