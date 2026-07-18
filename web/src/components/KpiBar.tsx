import { Row, Col, Card, Statistic, Alert, Skeleton, Typography } from 'antd'
import { getSummary } from '../api'
import { useApi } from '../useApi'

const { Text } = Typography

// fmtTime 把后端 ISO 时间串格式化为本地可读；空/非法 → 占位符（空库时 earliest/latest 为 null）。
function fmtTime(s: string | null): string {
  if (!s) return '—'
  const d = new Date(s)
  return Number.isNaN(d.getTime()) ? s : d.toLocaleString()
}

// KpiBar：顶部 KPI 概要条，字段对应 /api/summary。summary 不随时间窗变，仅随 refreshKey 重取。
export default function KpiBar({ refreshKey }: { refreshKey: number }) {
  const { data, error } = useApi(() => getSummary(), [refreshKey])

  if (error && !data) return <Alert type="error" showIcon message={`概要加载失败：${error}`} />
  if (!data) return <Skeleton active paragraph={{ rows: 2 }} />

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
            {fmtTime(data.earliest)} → {fmtTime(data.latest)}
          </Text>
        </Card>
      </Col>
    </Row>
  )
}
