import { Row, Col, Card, Statistic, Typography } from 'antd'
import { useNavigate } from 'react-router'
import { getSummary, getNewCount, detailPath, type Dimension, type Filter } from '../api'
import { useApi } from '../useApi'
import { fmtDateTime } from '../format'
import AsyncBody from './AsyncBody'

const { Text } = Typography

// KpiBar：顶部 KPI 概要条，字段对应 /api/summary（该过滤切片的口径）。summary 不随时间窗变，仅随切片/refreshKey 重取。
// 过滤型 KPI（带 delta）可点 → 在当前切片上叠加约束跳转过滤详情页；维度型（去重域名/App/节点）留待维度总览页。
export default function KpiBar({
  filter,
  since,
  refreshKey,
}: {
  filter: Filter
  since: string
  refreshKey: number
}) {
  const navigate = useNavigate()
  const state = useApi(() => getSummary(filter), [filter, refreshKey])

  // 「24h 新增」是 /new 的入口钩子。只在全集切片（主页）出现：新鲜度不接受过滤切片
  // （首次出现要拿窗口外历史当参照系），摆在详情页上会被误读成「该切片内的新增」。
  const isWholeSet = Object.keys(filter).length === 0
  const newCount = useApi(() => (isWholeSet ? getNewCount('24h') : Promise.resolve({ count: 0 })), [
    isWholeSet,
    refreshKey,
  ])

  return (
    <AsyncBody state={state} skeletonRows={2}>
      {(data) => {
        // delta = 过滤型（点→叠加约束进详情页）；dimTo = 维度型（点→该维度总览页）。
        const cards: { title: string; value: number; delta?: Filter; dimTo?: Dimension }[] = [
          { title: '总连接', value: data.total, delta: {} },
          { title: '去重域名', value: data.hosts, dimTo: 'host' },
          { title: 'App', value: data.processes, dimTo: 'process' },
          { title: '出境节点', value: data.nodes, dimTo: 'node' },
          { title: '直连', value: data.direct, delta: { route: 'direct' } },
          { title: '经代理', value: data.proxied, delta: { route: 'proxied' } },
          { title: 'HTTP · 80', value: data.http, delta: { port: 80 } },
          { title: 'HTTPS · 443', value: data.https, delta: { port: 443 } },
        ]
        return (
          <Row gutter={[16, 16]}>
            {cards.map((c) => {
              const onClick = c.dimTo
                ? () => navigate(`/d/${c.dimTo}?since=${encodeURIComponent(since)}`)
                : c.delta
                  ? () => navigate(detailPath({ ...filter, ...c.delta }, since))
                  : undefined
              return (
                <Col key={c.title} xs={12} sm={8} md={6} xl={3}>
                  <Card
                    size="small"
                    hoverable={onClick !== undefined}
                    style={onClick ? { cursor: 'pointer' } : undefined}
                    onClick={onClick}
                  >
                    <Statistic title={c.title} value={c.value} />
                  </Card>
                </Col>
              )
            })}
            {isWholeSet && (
              <Col xs={12} sm={8} md={6} xl={3}>
                {/* 与同排其余 KPI 一样：可点即钻取（点数字看它背后是哪些），不额外着色、不加箭头。
                    此前是红色 + 「→」——红色在本界面是危险语义（追踪器暴露那条进度条），
                    而「新增 183」不是危险状态；箭头则是 /new 尚无导航入口时的补偿，
                    现在顶级导航已承担该职责，留着只会让同类操作有两种长相。 */}
                <Card
                  size="small"
                  hoverable
                  style={{ cursor: 'pointer' }}
                  onClick={() => navigate('/new?since=24h')}
                >
                  <Statistic title="24h 新增" value={newCount.data ? newCount.data.count : '—'} />
                </Card>
              </Col>
            )}
            <Col xs={24}>
              <Card size="small">
                <Text type="secondary">时间跨度：</Text>
                <Text>
                  {fmtDateTime(data.earliest)} → {fmtDateTime(data.latest)}
                </Text>
              </Card>
            </Col>
          </Row>
        )
      }}
    </AsyncBody>
  )
}
