// 统一的时间格式化：跟随 UI 语言（简体中文）、24 小时制，
// 避免浏览器默认 locale 在中文界面里渲染出美式 "7/19/2026, 12:15 PM"。

// fmtDateTime：完整日期时间（用于时间跨度、tooltip 标题）。空/非法 → 占位符。
export function fmtDateTime(s: string | null): string {
  if (!s) return '—'
  const d = new Date(s)
  return Number.isNaN(d.getTime()) ? s : d.toLocaleString('zh-CN', { hour12: false })
}

// fmtTimeShort：只显示时:分（用于时间曲线 x 轴刻度，避免拥挤）。
export function fmtTimeShort(s: string): string {
  const d = new Date(s)
  return Number.isNaN(d.getTime())
    ? s
    : d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
}
