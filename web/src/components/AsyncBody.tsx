import type { ReactNode } from 'react'
import { Alert, Skeleton, Empty } from 'antd'
import type { ApiState } from '../useApi'

interface AsyncBodyProps<T> {
  state: ApiState<T>
  skeletonRows?: number
  isEmpty?: (data: T) => boolean
  emptyText?: string
  children: (data: T) => ReactNode
}

// AsyncBody：统一渲染 useApi 的四态，供各面板复用（消除重复分支）。
// - 首屏（无 data）：出错 Alert / 否则 Skeleton。
// - 有 data 后再出错（如轮询失败）：顶部细条 warning 提示 + 继续展示旧 data
//   （既不闪烁，也不把持续失败静默吞掉）。
export default function AsyncBody<T>({
  state,
  skeletonRows = 4,
  isEmpty,
  emptyText = '暂无数据',
  children,
}: AsyncBodyProps<T>) {
  const { data, error } = state

  if (!data) {
    return error ? (
      <Alert type="error" showIcon message={error} />
    ) : (
      <Skeleton active paragraph={{ rows: skeletonRows }} />
    )
  }

  const empty = isEmpty ? isEmpty(data) : false
  return (
    <>
      {error && (
        <Alert
          type="warning"
          showIcon
          banner
          message={`刷新失败：${error}（下方为上次数据）`}
          style={{ marginBottom: 8 }}
        />
      )}
      {empty ? <Empty description={emptyText} image={Empty.PRESENTED_IMAGE_SIMPLE} /> : children(data)}
    </>
  )
}
