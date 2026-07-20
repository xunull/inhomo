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

// truncate：截断过长分类标签（emoji 节点名 / 长 URL），完整值仍由 Tooltip 展示，避免溢出。
// 按「码点」而非 UTF-16 码元切：Array.from 把 emoji / 国旗（代理对）当单个字符，
// 避免从中间切断产生乱码「�」。
export function truncate(v: string, n = 12): string {
  const chars = Array.from(v)
  return chars.length > n ? chars.slice(0, n - 1).join('') + '…' : v
}

// fmtBytes：字节数友好显示（B/KB/MB/GB/TB，1024 进制）。用于流量视图的字节量。
export function fmtBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}
