import { useEffect, useRef } from 'react'
import * as echarts from 'echarts/core'
import { SankeyChart } from 'echarts/charts'
import { TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { FlowGraph } from '../api'

// 只注册用到的：Sankey + Tooltip + Canvas 渲染器（tree-shaking，随本页懒加载成独立 chunk）。
echarts.use([SankeyChart, TooltipComponent, CanvasRenderer])

// 由 echarts 回调传入的松散参数（节点 data 携 label/dim/key，供 tooltip 与 T26 点击钻取）。
type EchartsParam = {
  dataType?: string
  name: string
  data: { source?: string; target?: string; value?: number; label?: string }
}

// TopologyChart：用裸 echarts 画 App→节点 的 Sankey。节点 name 已带层前缀命名空间（后端保证不塌陷）。
export default function TopologyChart({ graph, height = 520 }: { graph: FlowGraph; height?: number }) {
  const elRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<ReturnType<typeof echarts.init> | null>(null)

  // 初始化 / 销毁 + 跟随窗口 resize（只做一次）。
  useEffect(() => {
    if (!elRef.current) return
    const chart = echarts.init(elRef.current)
    chartRef.current = chart
    const onResize = () => chart.resize()
    window.addEventListener('resize', onResize)
    return () => {
      window.removeEventListener('resize', onResize)
      chart.dispose()
      chartRef.current = null
    }
  }, [])

  // 数据变化时重设 option。
  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    // 边 tooltip 去掉节点名的层前缀（process:/node:），显示友好名。
    const strip = (s?: string) => (s ?? '').replace(/^[^:]+:/, '')
    chart.setOption(
      {
        tooltip: {
          trigger: 'item',
          formatter: (p: EchartsParam) =>
            p.dataType === 'edge'
              ? `${strip(p.data.source)} → ${strip(p.data.target)}：${p.data.value}`
              : (p.data.label ?? p.name),
        },
        series: [
          {
            type: 'sankey',
            data: graph.nodes.map((n) => ({ name: n.name, label: n.label, dim: n.dim, key: n.key })),
            links: graph.links,
            emphasis: { focus: 'adjacency' },
            nodeGap: 12,
            label: { formatter: (p: EchartsParam) => p.data.label ?? p.name, overflow: 'truncate', width: 140 },
            lineStyle: { color: 'gradient', opacity: 0.45 },
          },
        ],
      },
      // notMerge：全量替换，避免数据变小时残留上一次的幽灵节点/边。
      { notMerge: true },
    )
  }, [graph])

  return <div ref={elRef} style={{ width: '100%', height }} />
}
