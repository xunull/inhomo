import { useEffect, useState } from 'react'
import type { DependencyList } from 'react'

export interface ApiState<T> {
  data: T | null
  error: string | null
}

// useApi：deps 变化时重新取数。重取时保留上一次 data、只清 error（不闪烁）；
// 请求失败保留旧 data 并给出 error（一次瞬时失败不清空面板，由 AsyncBody 显示"刷新失败"提示）。
// fetcher 每次渲染新建，故不入依赖数组，触发条件完全由调用方的 deps 声明。
export function useApi<T>(fetcher: () => Promise<T>, deps: DependencyList): ApiState<T> {
  const [state, setState] = useState<ApiState<T>>({ data: null, error: null })
  useEffect(() => {
    let alive = true
    setState((s) => ({ ...s, error: null }))
    fetcher()
      .then((data) => {
        if (alive) setState({ data, error: null })
      })
      .catch((e: unknown) => {
        if (alive) {
          setState((s) => ({ ...s, error: e instanceof Error ? e.message : String(e) }))
        }
      })
    return () => {
      alive = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)
  return state
}
