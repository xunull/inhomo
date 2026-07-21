// format.ts 纯函数单测：字节/时间/截断的边界行为兜底。
// 显式从 vitest 导入（不开 globals），避免污染 app 类型。
import { describe, it, expect } from 'vitest'
import { fmtBytes, truncate, fmtDateTime, fmtTimeShort } from './format'

describe('fmtBytes', () => {
  // 非正常/非正数一律回落到 "0 B"，绝不显示 "NaN B" / 负字节。
  it('0 / 负 / NaN / Infinity → "0 B"', () => {
    expect(fmtBytes(0)).toBe('0 B')
    expect(fmtBytes(-5)).toBe('0 B')
    expect(fmtBytes(-1024)).toBe('0 B')
    expect(fmtBytes(NaN)).toBe('0 B')
    expect(fmtBytes(Infinity)).toBe('0 B')
    expect(fmtBytes(-Infinity)).toBe('0 B')
  })

  // < 1024 字节：整数、无小数、单位 B。
  it('< 1024 → "N B"（无小数）', () => {
    expect(fmtBytes(1)).toBe('1 B')
    expect(fmtBytes(512)).toBe('512 B')
    expect(fmtBytes(1023)).toBe('1023 B')
  })

  // 恰好跨到 1024 → 1.0 KB（进档、一位小数）。
  it('1024 → "1.0 KB"', () => {
    expect(fmtBytes(1024)).toBe('1.0 KB')
  })

  // KB 段一位小数。
  it('KB 一位小数', () => {
    expect(fmtBytes(1536)).toBe('1.5 KB')
    expect(fmtBytes(10240)).toBe('10.0 KB')
  })

  // 逐档跨越 MB/GB/TB。
  it('MB / GB / TB 跨档', () => {
    expect(fmtBytes(1024 ** 2)).toBe('1.0 MB')
    expect(fmtBytes(1024 ** 3)).toBe('1.0 GB')
    expect(fmtBytes(1024 ** 4)).toBe('1.0 TB')
    expect(fmtBytes(2.5 * 1024 ** 3)).toBe('2.5 GB')
  })

  // 超过 TB 不再进档，封顶停在 TB。
  it('超过 TB 封顶在 TB', () => {
    expect(fmtBytes(1024 ** 5)).toBe('1024.0 TB')
  })
})

describe('truncate', () => {
  // 长度 ≤ n 原样返回，不加省略号。
  it('长度 ≤ n 原样', () => {
    expect(truncate('short')).toBe('short')
    expect(truncate('')).toBe('')
    expect(truncate('abcdefghijkl', 12)).toBe('abcdefghijkl') // 恰好 12
    expect(truncate('abc', 3)).toBe('abc')
  })

  // 长度 > n 截到 n-1 个字符 + 省略号（总视觉长度 = n）。
  it('长度 > n 加省略号', () => {
    expect(truncate('abcdefghijklm', 12)).toBe('abcdefghijk…') // 13 → 11 + …
    expect(truncate('abcd', 3)).toBe('ab…')
  })

  // 默认 n=12。
  it('默认 n=12', () => {
    expect(truncate('a'.repeat(20))).toBe('a'.repeat(11) + '…')
  })

  // 回归：码点安全——emoji 是代理对（UTF-16 两个码元）。旧实现 String.slice(0, n-1)
  // 会在码元 1 处切开 😀，留下孤立高代理 '\uD83D'（渲染为乱码 �）；Array.from 按码点切，绝不切碎。
  it('emoji（代理对）不被从中间切碎', () => {
    // 3 个 emoji（各 1 码点 / 2 码元），n=2 → 取 1 个完整 emoji + …
    expect(truncate('😀😀😀', 2)).toBe('😀…')
    // 直接钉「无孤立代理」：旧码元切法在此会产出孤立高代理，令下式为 true 而失败。
    expect(hasLoneSurrogate(truncate('😀😀😀', 2))).toBe(false)
  })

  // 回归：国旗节点名。刻意取 n=4 —— 旧 String.slice(0,3) 恰好切在首面旗第一个区域指示符的
  // 代理对中间，会留下孤立代理；Array.from 按码点切，最坏只是把一面旗拆成两个指示符字母
  // （字形层面拆分可接受），但绝不产生孤立代理。
  it('国旗节点名截断不产生孤立代理', () => {
    const flags = '🇨🇳🇺🇸🇯🇵🇬🇧🇩🇪🇫🇷' // 6 面国旗 = 12 码点 / 24 码元
    const out = truncate(flags, 4)
    expect(out.endsWith('…')).toBe(true)
    expect(hasLoneSurrogate(out)).toBe(false)
  })
})

describe('fmtDateTime', () => {
  // 空/null → 占位符。
  it('null / 空串 → "—"', () => {
    expect(fmtDateTime(null)).toBe('—')
    expect(fmtDateTime('')).toBe('—')
  })

  // 非法日期 → 原样回显（不吞掉原始值，便于排错）。
  it('非法 → 原样', () => {
    expect(fmtDateTime('not-a-date')).toBe('not-a-date')
    expect(fmtDateTime('2026-13-99')).toBe('2026-13-99')
  })

  // 有效日期：只做宽松/结构断言，不钉死本地化字符串（随 Node ICU 版本会有细微差异）。
  it('有效 → 非占位、含年份、24 小时制（无 AM/PM）', () => {
    const out = fmtDateTime('2026-07-21T12:00:00Z') // 正午，任意时区都落在 2026 年
    expect(out).not.toBe('—')
    expect(out).not.toBe('2026-07-21T12:00:00Z')
    expect(out).toContain('2026')
    expect(out).not.toMatch(/AM|PM|上午|下午/i)
  })
})

describe('fmtTimeShort', () => {
  // 非法 → 原样。
  it('非法 → 原样', () => {
    expect(fmtTimeShort('nope')).toBe('nope')
  })

  // 有效 → HH:mm 形态（不断言具体时分，避免受运行时区影响）。
  it('有效 → HH:mm 形态', () => {
    const out = fmtTimeShort('2026-07-21T09:05:00Z')
    expect(out).toMatch(/^\d{1,2}:\d{2}$/)
  })
})

// 检测字符串是否含「孤立代理」——即被从中间切碎的代理对残片。
// 合法的高代理必须紧跟一个低代理；任何不成对的代理即为切碎证据。
function hasLoneSurrogate(s: string): boolean {
  for (let i = 0; i < s.length; i++) {
    const code = s.charCodeAt(i)
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = s.charCodeAt(i + 1)
      if (!(next >= 0xdc00 && next <= 0xdfff)) return true
      i++ // 跳过已配对的低代理
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      return true // 孤立低代理
    }
  }
  return false
}
