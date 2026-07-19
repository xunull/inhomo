import { useState } from 'react'
import { Card, Table, Alert, type TableColumnsType } from 'antd'
import { getConnections, type ConnRow, type Filter } from '../api'
import { useApi } from '../useApi'
import { fmtDateTime } from '../format'

const PAGE = 50

const columns: TableColumnsType<ConnRow> = [
  { title: '时间', dataIndex: 'ts', key: 'ts', width: 190, render: (v: string) => fmtDateTime(v) },
  { title: '进程', dataIndex: 'process', key: 'process', render: (v: string) => v || '(未知)' },
  { title: '目的', key: 'dst', render: (_, r) => `${r.host}:${r.port}` },
  { title: '网络', dataIndex: 'network', key: 'network', width: 72 },
  { title: '命中规则', dataIndex: 'rule', key: 'rule' },
  { title: '出境节点', dataIndex: 'node', key: 'node' },
  { title: '地区', dataIndex: 'region', key: 'region', width: 84 },
]

// ConnTable：某过滤切片的原始连接明细表（时间倒序、分页、显示共 N 条）。
export default function ConnTable({
  filter,
  since,
  refreshKey,
}: {
  filter: Filter
  since: string
  refreshKey: number
}) {
  // 分页回到第一页由父级 key 重挂载负责（切片/时间窗变化 → 组件重建、page 归 1）。
  const [page, setPage] = useState(1)
  const { data, error } = useApi(
    () => getConnections(filter, since, (page - 1) * PAGE, PAGE),
    [filter, since, page, refreshKey],
  )

  return (
    <Card title="连接明细" size="small">
      {error && (
        <Alert
          type={data ? 'warning' : 'error'}
          showIcon
          banner
          message={data ? `刷新失败：${error}（下方为上次数据）` : error}
          style={{ marginBottom: 8 }}
        />
      )}
      <Table<ConnRow>
        size="small"
        rowKey={(r) => `${r.ts}|${r.host}|${r.port}|${r.process}`}
        columns={columns}
        dataSource={data?.rows ?? []}
        loading={!data && !error}
        pagination={{
          current: page,
          pageSize: PAGE,
          total: data?.total ?? 0,
          onChange: setPage,
          showTotal: (t) => `共 ${t} 条`,
          showSizeChanger: false,
        }}
        scroll={{ x: 'max-content' }}
      />
    </Card>
  )
}
