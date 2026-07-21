// api.ts 纯函数单测：过滤切片 ↔ URL 编解码 + 钻取。
// 这些是钻取、分享链接、浏览器前进后退能不能对的根基；显式从 vitest 导入。
import { describe, it, expect } from 'vitest'
import {
  type Filter,
  filterParams,
  qs,
  filterFromParams,
  withDim,
  filterChips,
  withoutKey,
  detailPath,
  topologyPath,
  trafficPath,
} from './api'

describe('filterParams ↔ filterFromParams 往返不变量', () => {
  // 编码→解码能原样还原：钻取/分享链接的根不变量。port 必须回落为数字、route 保谓词。
  const CASES: Filter[] = [
    {},
    { host: 'example.com' },
    { process: 'curl' },
    { node: '🇯🇵 Tokyo-01' }, // unicode 节点名（含空格）也须原样还原
    { region: 'JP' },
    { port: 443 },
    { route: 'direct' },
    { route: 'proxied' },
    { host: 'a.com', process: 'firefox', node: 'US-1', region: 'US', port: 8080, route: 'proxied' },
  ]
  it.each(CASES.map((f) => [f]))('往返还原 %o', (f) => {
    // 经 .toString() 真正过一遍 URL 字符串（含百分号编码 / 空格→+），再解析回来——
    // 这才是分享链接、浏览器前进后退的真实路径；直接 get() 只在内存 map 里取、不过编码边界。
    expect(filterFromParams(new URLSearchParams(filterParams(f).toString()))).toEqual(f)
  })
})

describe('filterParams', () => {
  it('只带非空约束（空串不落参）', () => {
    const p = filterParams({ host: 'x.com', process: '', port: 443 })
    expect(p.get('host')).toBe('x.com')
    expect(p.has('process')).toBe(false)
    expect(p.get('port')).toBe('443')
  })

  it('route 落参', () => {
    expect(filterParams({ route: 'direct' }).get('route')).toBe('direct')
  })

  it('空切片 → 空参数', () => {
    expect(filterParams({}).toString()).toBe('')
  })
})

describe('qs', () => {
  it('合并 extra、丢空值、带 ? 前缀', () => {
    const s = qs({ host: 'x.com' }, { by: 'node', metric: 'total', since: '', limit: 20 })
    expect(s.startsWith('?')).toBe(true)
    const p = new URLSearchParams(s.slice(1))
    expect(p.get('host')).toBe('x.com')
    expect(p.get('by')).toBe('node')
    expect(p.get('metric')).toBe('total')
    expect(p.has('since')).toBe(false) // 空串丢弃
    expect(p.get('limit')).toBe('20') // 数字转字符串
  })

  it('undefined 也丢弃', () => {
    expect(qs({}, { by: undefined, since: '1h' })).toBe('?since=1h')
  })

  it('全空 → 空串（无 ?）', () => {
    expect(qs({})).toBe('')
    expect(qs({}, { since: '' })).toBe('')
  })
})

describe('filterFromParams', () => {
  it('port 转数字', () => {
    expect(filterFromParams(new URLSearchParams('port=443')).port).toBe(443)
  })

  it('非法 port 忽略', () => {
    expect(filterFromParams(new URLSearchParams('port=abc')).port).toBeUndefined()
  })

  it('只接受合法 route', () => {
    expect(filterFromParams(new URLSearchParams('route=direct')).route).toBe('direct')
    expect(filterFromParams(new URLSearchParams('route=proxied')).route).toBe('proxied')
    expect(filterFromParams(new URLSearchParams('route=weird')).route).toBeUndefined()
  })

  it('忽略未知/空参数', () => {
    expect(filterFromParams(new URLSearchParams('host=x.com&foo=bar'))).toEqual({ host: 'x.com' })
  })
})

describe('withDim 钻取', () => {
  it('port → Number', () => {
    expect(withDim({}, 'port', '443')).toEqual({ port: 443 })
  })

  it('其余维度 → string', () => {
    expect(withDim({}, 'host', 'x.com')).toEqual({ host: 'x.com' })
    expect(withDim({}, 'region', 'JP')).toEqual({ region: 'JP' })
  })

  it('同维再叠加 = 替换', () => {
    expect(withDim({ host: 'a.com' }, 'host', 'b.com')).toEqual({ host: 'b.com' })
    expect(withDim({ port: 80 }, 'port', '443')).toEqual({ port: 443 })
  })

  it('保留其它维度、不改原对象', () => {
    const f: Filter = { region: 'JP' }
    const next = withDim(f, 'host', 'x.com')
    expect(next).toEqual({ region: 'JP', host: 'x.com' })
    expect(f).toEqual({ region: 'JP' })
    expect(next).not.toBe(f)
  })
})

describe('filterChips', () => {
  it('维度标签映射 + 每片带可删除的 key', () => {
    const chips = filterChips({ host: 'x.com', process: 'curl', node: 'N1', region: 'JP', port: 443 })
    expect(chips).toEqual([
      { key: 'host', label: '域名', value: 'x.com' },
      { key: 'process', label: 'App', value: 'curl' },
      { key: 'node', label: '节点', value: 'N1' },
      { key: 'region', label: '地区', value: 'JP' },
      { key: 'port', label: '端口', value: '443' },
    ])
  })

  it('route 翻译成 直连/经代理', () => {
    expect(filterChips({ route: 'direct' })).toEqual([{ key: 'route', label: '类型', value: '直连' }])
    expect(filterChips({ route: 'proxied' })).toEqual([{ key: 'route', label: '类型', value: '经代理' }])
  })

  it('空切片 → 无 chip', () => {
    expect(filterChips({})).toEqual([])
  })
})

describe('withoutKey', () => {
  it('删除指定约束', () => {
    expect(withoutKey({ host: 'x.com', port: 443 }, 'host')).toEqual({ port: 443 })
  })

  it('返回新对象、不改原对象（不可变）', () => {
    const f: Filter = { host: 'x.com', route: 'proxied' }
    const next = withoutKey(f, 'route')
    expect(next).toEqual({ host: 'x.com' })
    expect(f).toEqual({ host: 'x.com', route: 'proxied' })
    expect(next).not.toBe(f)
  })
})

describe('detailPath / topologyPath / trafficPath', () => {
  // 前缀由下方各精确相等断言（/detail、/topology、/traffic + 带参用例）一并钉住，无需单独的 startsWith 弱断言。
  it('空切片、无 since → 纯前缀（无 ?）', () => {
    expect(detailPath({})).toBe('/detail')
    expect(topologyPath({})).toBe('/topology')
    expect(trafficPath({})).toBe('/traffic')
  })

  it('编码过滤切片（特殊字符不裸露在 URL 里）', () => {
    const url = detailPath({ host: 'a b.com', port: 443 })
    const [prefix, query] = url.split('?')
    expect(prefix).toBe('/detail')
    expect(query).not.toContain(' ') // 空格已编码（+ 或 %20）
    const p = new URLSearchParams(query)
    expect(p.get('host')).toBe('a b.com') // 解码回原值
    expect(p.get('port')).toBe('443')
  })

  it('可选 since：传则带、不传则不带', () => {
    expect(topologyPath({ region: 'JP' }, '24h')).toBe('/topology?region=JP&since=24h')
    expect(topologyPath({ region: 'JP' })).toBe('/topology?region=JP')
    expect(trafficPath({}, '7d')).toBe('/traffic?since=7d')
  })
})
