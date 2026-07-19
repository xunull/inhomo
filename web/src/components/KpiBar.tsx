import { Row, Col, Card, Statistic, Typography } from 'antd'
import { getSummary } from '../api'
import { useApi } from '../useApi'
import { fmtDateTime } from '../format'
import AsyncBody from './AsyncBody'

const { Text } = Typography

// KpiBar：顶部 KPI 概要条，字段对应 /api/summary。summary 不随时间窗变，仅随 refreshKey 重取。
export default function KpiBar({ refreshKey }: { refreshKey: number }) {
  const state = useApi(() => getSummary(), [refreshKey])

  return (
    <AsyncBody state={state} skeletonRows={2}>
      {(data) => {
        const cards: { title: string; value: number }[] = [
          { title: '总连接', value: data.total },
          { title: '去重域名', value: data.hosts },
          { title: 'App', value: data.processes },
          { title: '出境节点', value: data.nodes },
          { title: '直连', value: data.direct },
          { title: '经代理', value: data.proxied },
          { title: 'HTTP · 80', value: data.http },
          { title: 'HTTPS · 443', value: data.https },
        ]
        return (
          <Row gutter={[16, 16]}>
            {cards.map((c) => (
              <Col key={c.title} xs={12} sm={8} md={6} xl={3}>
                <Card size="small">
                  <Statistic title={c.title} value={c.value} />
                </Card>
              </Col>
            ))}
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
