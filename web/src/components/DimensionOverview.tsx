import { useState } from 'react'
import { useParams, useSearchParams, useNavigate, Link } from 'react-router'
import { Card, Table, Input, Space, Typography, Alert, type TableColumnsType } from 'antd'
import {
  getAggregate,
  detailPath,
  withDim,
  EMPTY_FILTER,
  FILTER_DIMS,
  type AggRow,
  type Dimension,
} from '../api'
import { useApi } from '../useApi'

const { Text } = Typography

// isDim：校验 URL 里的 :dim 是否是受支持的维度。
function isDim(s: string | undefined): s is Dimension {
  return FILTER_DIMS.some((d) => d.key === s)
}

type Row = { key: string; display: string; count: number; rank: number }

// 后端聚合上限（同 store.Aggregate 的 clamp）：超过则总览只覆盖前 CAP 名。
const CAP = 1000

// DimensionOverview：某维度的全量排名页（/d/:dim）。全部取值 + 计数 + 占比，可搜索排序；
// 每行点击 → 该取值的过滤详情页。是「看有哪些域名/App/节点」的落点与钻取中转。
export default function DimensionOverview() {
  const { dim } = useParams()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const [q, setQ] = useState('')

  const since = params.get('since') || '1h'
  const valid = isDim(dim)
  // 全量排名（大 limit）。无效维度不发请求；hooks 需无条件调用，故在 fetcher 内分支。
  const { data, error } = useApi(
    () => (valid ? getAggregate(dim, EMPTY_FILTER, since, CAP) : Promise.resolve([] as AggRow[])),
    [dim, since],
  )

  if (!valid) {
    return (
      <Alert
        type="error"
        showIcon
        message={`未知维度：${dim}`}
        description={<Link to="/">← 返回仪表盘</Link>}
      />
    )
  }
  const label = FILTER_DIMS.find((d) => d.key === dim)!.label

  // rank = 后端 count 降序位次（预算进行数据，跨分页/搜索/再排序都稳，不随页内 index 重置）。
  const all: Row[] = (data ?? []).map((r, i) => ({
    key: r.key,
    display: r.key || '(未知)',
    count: r.count,
    rank: i + 1,
  }))
  const total = all.reduce((s, r) => s + r.count, 0)
  const capped = all.length >= CAP // 命中聚合上限：总览与占比只覆盖前 CAP 名
  const rows = q ? all.filter((r) => r.display.toLowerCase().includes(q.toLowerCase())) : all

  const columns: TableColumnsType<Row> = [
    { title: '#', dataIndex: 'rank', key: 'rank', width: 56 },
    { title: label, dataIndex: 'display', key: 'display', ellipsis: true },
    {
      title: '连接数',
      dataIndex: 'count',
      key: 'count',
      width: 130,
      sorter: (a, b) => a.count - b.count,
      defaultSortOrder: 'descend',
    },
    {
      title: '占比',
      key: 'share',
      width: 110,
      render: (_v, r) => (total ? `${((r.count / total) * 100).toFixed(1)}%` : '—'),
    },
  ]

  return (
    <>
      <Space wrap align="center" style={{ marginBottom: 16 }}>
        <Link to="/">← 仪表盘</Link>
        <Text type="secondary">/</Text>
        <Text strong>{label}总览</Text>
      </Space>
      <Card
        size="small"
        title={`全部${label}（共 ${all.length} 项${capped ? `，仅前 ${CAP}、占比为近似` : ''}）`}
        extra={
          <Input.Search
            placeholder={`搜索${label}`}
            allowClear
            onChange={(e) => setQ(e.target.value)}
            style={{ width: 220 }}
          />
        }
      >
        {error && (
          <Alert
            type={data ? 'warning' : 'error'}
            showIcon
            banner
            message={data ? `刷新失败：${error}` : error}
            style={{ marginBottom: 8 }}
          />
        )}
        <Table<Row>
          size="small"
          rowKey="key"
          columns={columns}
          dataSource={rows}
          loading={!data && !error}
          pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 项` }}
          onRow={(r) => ({
            style: { cursor: r.key ? 'pointer' : 'default' },
            // 点行 → 该取值的过滤详情页（空 key 不可钻取）。
            onClick: () => {
              if (r.key) navigate(detailPath(withDim(EMPTY_FILTER, dim, r.key), since))
            },
          })}
        />
      </Card>
    </>
  )
}
