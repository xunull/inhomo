import { Row, Col, Card, Statistic, Typography } from 'antd'
import { useNavigate } from 'react-router'
import { getSummary, detailPath, type Filter } from '../api'
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

  return (
    <AsyncBody state={state} skeletonRows={2}>
      {(data) => {
        const cards: { title: string; value: number; delta?: Filter }[] = [
          { title: '总连接', value: data.total, delta: {} },
          { title: '去重域名', value: data.hosts },
          { title: 'App', value: data.processes },
          { title: '出境节点', value: data.nodes },
          { title: '直连', value: data.direct, delta: { route: 'direct' } },
          { title: '经代理', value: data.proxied, delta: { route: 'proxied' } },
          { title: 'HTTP · 80', value: data.http, delta: { port: 80 } },
          { title: 'HTTPS · 443', value: data.https, delta: { port: 443 } },
        ]
        return (
          <Row gutter={[16, 16]}>
            {cards.map((c) => {
              const clickable = c.delta !== undefined
              return (
                <Col key={c.title} xs={12} sm={8} md={6} xl={3}>
                  <Card
                    size="small"
                    hoverable={clickable}
                    style={clickable ? { cursor: 'pointer' } : undefined}
                    onClick={
                      clickable
                        ? () => navigate(detailPath({ ...filter, ...c.delta }, since))
                        : undefined
                    }
                  >
                    <Statistic title={c.title} value={c.value} />
                  </Card>
                </Col>
              )
            })}
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
