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
  FLOW_METRICS,
  flowMetricFromParams,
  isByteMetric,
  isFilterable,
  FILTER_DIMS,
  domainRule,
  ipRule,
  RULE_GROUP_PLACEHOLDER,
  GAP_WINDOWS,
  type AggDimension,
  type ExfilRow,
  coverageOf,
  isWellEvidenced,
  COVERAGE_MIN,
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

describe('flowMetricFromParams（拓扑度量 URL 解析）', () => {
  // URL 是拓扑度量的单一事实源：分享链接 / 刷新 / 前进后退都从这里还原；缺省 / 非法 → 连接数。
  it('缺省 → 连接数 count', () => {
    expect(flowMetricFromParams(new URLSearchParams(''))).toBe('count')
  })

  it('每个合法度量原样还原（与 FLOW_METRICS 白名单一致）', () => {
    for (const { value } of FLOW_METRICS) {
      expect(flowMetricFromParams(new URLSearchParams('metric=' + value))).toBe(value)
    }
  })

  it('白名单外 → 回落连接数', () => {
    expect(flowMetricFromParams(new URLSearchParams('metric=bogus'))).toBe('count')
  })

  it('空 metric= → 回落连接数', () => {
    expect(flowMetricFromParams(new URLSearchParams('metric='))).toBe('count')
  })
})

describe('isByteMetric（字节口径判定：驱动 fmtBytes 显示 + 抽样提示）', () => {
  it('连接数 → 非字节', () => {
    expect(isByteMetric('count')).toBe(false)
  })

  it('上行 / 下行 / 合计 → 字节', () => {
    expect(isByteMetric('up')).toBe(true)
    expect(isByteMetric('down')).toBe(true)
    expect(isByteMetric('total')).toBe(true)
  })
})

describe('FLOW_METRICS（拓扑度量选项单一事实源）', () => {
  it('连接数为列首（URL 无参时的默认口径）', () => {
    expect(FLOW_METRICS[0].value).toBe('count')
  })

  it('四个度量 + 中文标签，顺序 连接数/合计/上行/下行', () => {
    expect(FLOW_METRICS.map((m) => m.value)).toEqual(['count', 'total', 'up', 'down'])
    expect(FLOW_METRICS.map((m) => m.label)).toEqual(['连接数', '合计', '上行', '下行'])
  })
})

describe('采样覆盖率（外发比结论的证据基础，见 ADR-0013）', () => {
  const row = (sampled: number, logged: number): ExfilRow => ({
    process: 'p',
    host: 'h',
    up: 1,
    down: 1,
    ratio: 1,
    sampled,
    logged,
  })

  it('覆盖率 = 抽样行数 / 全量行数', () => {
    expect(coverageOf(row(10, 20))).toBe(0.5)
    expect(coverageOf(row(9, 10))).toBeCloseTo(0.9)
  })

  it('分母为 0（该通道没有连接事件）→ null，表示无从判断而非 0%', () => {
    expect(coverageOf(row(3, 0))).toBeNull()
  })

  it('达到下限算证据充分，低于下限算不足', () => {
    expect(isWellEvidenced(row(COVERAGE_MIN * 100, 100))).toBe(true)
    expect(isWellEvidenced(row(COVERAGE_MIN * 100 - 1, 100))).toBe(false)
  })

  it('无从判断覆盖率的行按证据不足处理（不给它免检）', () => {
    expect(isWellEvidenced(row(3, 0))).toBe(false)
  })
})

describe('isFilterable（可聚合 ≠ 可过滤：杜绝把规则当过滤条件）', () => {
  // 后端 aggDimensions 白名单比可过滤维度多一个 rule；store.Filter 有意没有 Rule 字段。
  // 若前端把 rule 当过滤约束发出去，后端会**静默忽略**它——用户看到的是全量数据，
  // 却以为已经筛过了。这条不变量就是把那种情况挡在类型层面。
  it('可过滤维度全部为 true，且与 FILTER_DIMS 一一对应', () => {
    for (const d of FILTER_DIMS) {
      expect(isFilterable(d.key)).toBe(true)
    }
  })

  it('rule 为 false —— 它可聚合但不可过滤', () => {
    expect(isFilterable('rule')).toBe(false)
  })

  it('FILTER_DIMS 不含 rule（维度总览页 /d/:dim 据此拒绝 /d/rule）', () => {
    expect(FILTER_DIMS.some((d) => (d.key as AggDimension) === 'rule')).toBe(false)
  })
})

describe('规则片段生成（「补规则工作台」的产出）', () => {
  it('域名 → DOMAIN-SUFFIX，策略组留占位符', () => {
    // inhomo 拿不到策略组名（detect.go 的 effectiveNode 在解析时剥掉了分组名，见 ADR-0015），
    // 所以片段必须留占位符而非编一个——粘进去直接生效的假规则比留空更危险。
    expect(domainRule('anthropic.com')).toBe(`  - DOMAIN-SUFFIX,anthropic.com,${RULE_GROUP_PLACEHOLDER}`)
  })

  it('IPv4 → IP-CIDR /32', () => {
    expect(ipRule('192.0.2.1')).toBe(`  - IP-CIDR,192.0.2.1/32,${RULE_GROUP_PLACEHOLDER},no-resolve`)
  })

  it('IPv6 → IP-CIDR6 /128，且**去掉方括号**', () => {
    // 库里的 IPv6 host 带方括号（如 [2620:149:af6::10]），照原样写进规则是无效语法。
    expect(ipRule('[2620:149:af6::10]')).toBe(
      `  - IP-CIDR6,2620:149:af6::10/128,${RULE_GROUP_PLACEHOLDER},no-resolve`,
    )
  })

  it('不带方括号的 IPv6 同样处理', () => {
    expect(ipRule('2620:149:af6::10')).toContain('IP-CIDR6,2620:149:af6::10/128')
  })
})

describe('GAP_WINDOWS（规则缺口的时间窗）', () => {
  it('默认档位含「全部时间」（空串）', () => {
    expect(GAP_WINDOWS.some((w) => w.value === '')).toBe(true)
  })

  it('7d 在列表里——它是默认档，早补过规则的域名不该被全时段翻出来', () => {
    expect(GAP_WINDOWS.map((w) => w.value)).toContain('7d')
  })
})
