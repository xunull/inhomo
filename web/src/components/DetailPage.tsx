import { useMemo } from 'react'
import { useSearchParams, Link } from 'react-router'
import { Tag, Space, Typography } from 'antd'
import { filterFromParams, filterChips } from '../api'
import Dashboard from './Dashboard'

const { Text } = Typography

// DetailPage：某过滤切片的详情页 = 面包屑 + 迷你仪表盘（隐藏钉死维）+ 原始明细表。
export default function DetailPage() {
  const [params] = useSearchParams()
  // 从 URL 还原过滤切片；用 useMemo 按查询串缓存，保证 URL 不变时引用稳定，
  // 避免子组件 useApi 依赖数组（含 filter 引用）每次渲染误触发重取。
  const paramKey = params.toString()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const filter = useMemo(() => filterFromParams(params), [paramKey])
  const since = params.get('since') || '1h'
  const tags = filterChips(filter)

  return (
    <>
      <Space wrap align="center" style={{ marginBottom: 16 }}>
        <Link to="/">← 仪表盘</Link>
        <Text type="secondary">/</Text>
        {tags.length === 0 ? (
          <Text type="secondary">全部连接</Text>
        ) : (
          tags.map((t) => (
            <Tag key={t.label} color="blue">
              {t.label}：{t.value}
            </Tag>
          ))
        )}
      </Space>
      <Dashboard filter={filter} initialSince={since} initialAuto={false} showConnections />
    </>
  )
}
