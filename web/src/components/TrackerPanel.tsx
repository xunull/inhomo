import { Card, Progress, Typography, Empty, Alert } from 'antd'
import { getTrackers, type Filter } from '../api'
import { useApi } from '../useApi'
import AsyncBody from './AsyncBody'
import HBarChart from './HBarChart'

const { Text } = Typography

interface TrackerPanelProps {
  filter: Filter
  since: string
  refreshKey: number
  limit?: number
}

// TrackerPanel：切片内「多少连接走了已知追踪器」+ 按归属公司 top-N。
// 数据来自本机离线追踪器表（`inhomo tracker update` 拉取）；未拉取则占比 0、列表空并提示。
// 归属公司不是可过滤维度（后端 Filter 无 owner），故条形不做钻取（HBarChart 不传 onBarClick）。
export default function TrackerPanel({ filter, since, refreshKey, limit = 8 }: TrackerPanelProps) {
  const state = useApi(
    () => getTrackers(filter, since, limit),
    [filter, since, limit, refreshKey],
  )

  return (
    <Card title="追踪器暴露" size="small" styles={{ body: { padding: 12 } }}>
      <AsyncBody state={state} skeletonRows={4} isEmpty={(d) => d.total === 0}>
        {(data) => {
          const pct = data.total > 0 ? Math.round((data.tracker / data.total) * 1000) / 10 : 0
          // 后端归属可能为空串（命中但数据无归属名），前端在此贴标签。
          const rows = data.owners.map((o) => ({ label: o.owner || '（未知归属）', value: o.count }))
          const height = Math.max(120, rows.length * 30)
          return (
            <>
              {!data.loaded && (
                <Alert
                  type="info"
                  showIcon
                  message="未拉取追踪器数据"
                  description={
                    <>
                      运行 <code>inhomo tracker update</code> 后刷新，即可识别已知追踪器并显示归属公司。
                    </>
                  }
                  style={{ marginBottom: 12 }}
                />
              )}
              <Text>
                {data.tracker.toLocaleString()} / {data.total.toLocaleString()} 条连接命中已知追踪器
              </Text>
              <Progress
                percent={pct}
                strokeColor="#cf1322"
                style={{ marginTop: 4 }}
                aria-label="已知追踪器连接占比"
              />
              {rows.length > 0 ? (
                <div style={{ marginTop: 12 }}>
                  <HBarChart rows={rows} color="#cf1322" height={height} />
                </div>
              ) : (
                // 已加载但零命中 → 明确「没追踪器」；未加载已有上面的 Alert，不再放空框。
                data.loaded && (
                  <Empty
                    description="未发现已知追踪器"
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    style={{ marginTop: 12 }}
                  />
                )
              )}
            </>
          )
        }}
      </AsyncBody>
    </Card>
  )
}
