import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router'
import { Alert, Button, Card, Select, Space, Table, Tag, Tooltip, Typography, message } from 'antd'
import {
  getGaps,
  domainRule,
  ipRule,
  GAP_WINDOWS,
  type FallthroughHost,
  type GapsResp,
  type RuleGap,
} from '../api'
import { useApi } from '../useApi'
import { fmtBytes, fmtDateTime } from '../format'
import AsyncBody from './AsyncBody'

const { Text, Title, Paragraph } = Typography

// GapRow 是表格行：缺口本身 + 预先算好的累计覆盖（按当前排序自上而下累加）。
type GapRow = RuleGap & { cumConns: number; cumBytes: number }

// withCumulative：按已排好的顺序累加，得出「读到这行为止已覆盖多少兜底流量」。
// 分母含 IP 目标区，故最后一行也到不了 100%——差额就是 IP 区，这是诚实的。
function withCumulative(gaps: RuleGap[]): GapRow[] {
  let c = 0
  let b = 0
  return gaps.map((g) => {
    c += g.conns
    b += g.bytes
    return { ...g, cumConns: c, cumBytes: b }
  })
}

// copy：复制到剪贴板。非安全上下文下 navigator.clipboard 不可用，退回提示用户手动复制。
async function copy(text: string, ok: () => void, fail: () => void) {
  try {
    await navigator.clipboard.writeText(text)
    ok()
  } catch {
    fail()
  }
}

export default function GapsPage() {
  const [params, setParams] = useSearchParams()
  const [refreshKey, setRefreshKey] = useState(0)
  const [selected, setSelected] = useState<string[]>([])
  const [msg, ctx] = message.useMessage()

  const raw = params.get('since')
  const since = useMemo(
    () => (GAP_WINDOWS.some((w) => w.value === (raw ?? '')) && raw !== null ? raw : '7d'),
    [raw],
  )
  const state = useApi(() => getGaps(since), [since, refreshKey])

  const setSince = (v: string) => {
    const next = new URLSearchParams(params)
    next.set('since', v)
    setParams(next, { replace: true })
  }

  const copyRules = (lines: string[]) => {
    if (lines.length === 0) return
    copy(
      lines.join('\n'),
      () => msg.success(`已复制 ${lines.length} 条规则`),
      () => msg.error('复制失败（浏览器未授予剪贴板权限），请手动选中复制'),
    )
  }

  return (
    <>
      {ctx}
      <Space wrap align="center" style={{ marginBottom: 16 }}>
        {/* 顶级页面的身份与返回由 Header 导航承担（当前项已高亮），此处不再重复。
            钻取页 /detail、/d/:dim 不在导航里，它们各自保留返回条。 */}
        <Text type="secondary">时间窗</Text>
        <Select value={since} onChange={setSince} options={GAP_WINDOWS} style={{ width: 130 }} />
        <Button onClick={() => setRefreshKey((k) => k + 1)}>立即刷新</Button>
      </Space>

      <Title level={5} style={{ marginTop: 0 }}>
        我该先补哪几条分流规则
      </Title>
      <Paragraph type="secondary" style={{ marginBottom: 16 }}>
        下列域名的连接<Text strong>没有被任何具体规则命中</Text>，只是落到了规则集末尾的兜底规则。
        每行 = 一条待补的规则。按字节降序——一个走代理下了 1&nbsp;GB 的域名，比几千条遥测小连接更该补。
      </Paragraph>

      <AsyncBody
        state={state}
        skeletonRows={8}
        isEmpty={(d: GapsResp) => d.gaps.length === 0 && d.ipTargets.length === 0}
        emptyText="该时间窗内没有兜底连接——你的规则集覆盖得很完整"
      >
        {(data) => {
          const rows = withCumulative(data.gaps)
          const selectedRules = rows.filter((r) => selected.includes(r.domain)).map((r) => domainRule(r.domain))
          return (
            <>
              {data.bypassed > 0 && (
                <Alert
                  type="warning"
                  showIcon
                  style={{ marginBottom: 16 }}
                  message={`另有 ${data.bypassed.toLocaleString()} 条连接未经规则匹配（GLOBAL 模式 / specialProxy）`}
                  description="那些连接根本没走规则匹配，补规则对它们不会生效，故未列入下表。"
                />
              )}

              <Card size="small" style={{ marginBottom: 16 }}>
                <Space wrap>
                  <Text type="secondary">
                    共 {data.gaps.length} 条缺口 · {data.totalConns.toLocaleString()} 条兜底连接 ·{' '}
                    {fmtBytes(data.totalBytes)}
                  </Text>
                  <Button
                    type="primary"
                    size="small"
                    disabled={selected.length === 0}
                    onClick={() => copyRules(selectedRules)}
                  >
                    复制选中的 {selected.length} 条规则
                  </Button>
                </Space>
              </Card>

              <Table<GapRow>
                size="small"
                rowKey="domain"
                dataSource={rows}
                pagination={rows.length > 50 ? { pageSize: 50, size: 'small' } : false}
                rowSelection={{
                  selectedRowKeys: selected,
                  onChange: (keys) => setSelected(keys as string[]),
                }}
                expandable={{
                  // 展开看这条规则实际会盖住哪些子域——写宽泛规则前的核实步骤。
                  expandedRowRender: (g) => (
                    <Table<FallthroughHost>
                      size="small"
                      rowKey="host"
                      dataSource={g.hosts}
                      pagination={false}
                      columns={[
                        { title: '子域', key: 'h', render: (_, h) => <Text>{h.host}</Text> },
                        { title: '连接数', key: 'c', align: 'right', width: 90, render: (_, h) => h.conns },
                        { title: '字节', key: 'b', align: 'right', width: 110, render: (_, h) => fmtBytes(h.bytes) },
                      ]}
                    />
                  ),
                  rowExpandable: (g) => g.hosts.length > 0,
                }}
                columns={[
                  {
                    title: '可注册域',
                    key: 'domain',
                    render: (_: unknown, g: GapRow) => <Text strong>{g.domain}</Text>,
                  },
                  {
                    title: '字节',
                    key: 'bytes',
                    align: 'right' as const,
                    width: 110,
                    render: (_: unknown, g: GapRow) => fmtBytes(g.bytes),
                  },
                  {
                    title: '连接数',
                    key: 'conns',
                    align: 'right' as const,
                    width: 90,
                    render: (_: unknown, g: GapRow) => g.conns.toLocaleString(),
                  },
                  {
                    title: '累计覆盖',
                    key: 'cum',
                    align: 'right' as const,
                    width: 110,
                    render: (_: unknown, g: GapRow) => (
                      <Tooltip title={`按字节：读到这行已覆盖 ${g.cumBytes.toLocaleString()} / ${data.totalBytes.toLocaleString()} 字节`}>
                        <Text type="secondary">
                          {data.totalBytes > 0 ? `${Math.round((g.cumBytes / data.totalBytes) * 100)}%` : '—'}
                        </Text>
                      </Tooltip>
                    ),
                  },
                  {
                    title: '最后兜底',
                    key: 'last',
                    width: 160,
                    render: (_: unknown, g: GapRow) => <Text type="secondary">{fmtDateTime(g.lastTs)}</Text>,
                  },
                  {
                    title: '规则片段',
                    key: 'rule',
                    width: 110,
                    render: (_: unknown, g: GapRow) => (
                      <Button size="small" onClick={() => copyRules([domainRule(g.domain)])}>
                        复制
                      </Button>
                    ),
                  },
                ]}
              />

              <Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 8 }}>
                字节来自<Text strong>抽样</Text>的流量记录（短连接会漏，见 ADR-0008），按 host 关联；连接数是全量。
                规则片段里的 <code>&lt;策略组&gt;</code> 需你自行替换成配置里的策略组名——inhomo 拿不到它。
              </Text>

              {data.ipTargets.length > 0 && (
                <Card
                  size="small"
                  style={{ marginTop: 24 }}
                  title={
                    <Space>
                      <span>IP 目标</span>
                      <Tag>{data.ipTargets.length}</Tag>
                      <Text type="secondary" style={{ fontWeight: 400, fontSize: 12 }}>
                        取不到可注册域，写不了 DOMAIN-SUFFIX，规则片段用 IP-CIDR
                      </Text>
                    </Space>
                  }
                >
                  <Table<FallthroughHost>
                    size="small"
                    rowKey="host"
                    dataSource={data.ipTargets}
                    pagination={data.ipTargets.length > 20 ? { pageSize: 20, size: 'small' } : false}
                    columns={[
                      { title: '目标', key: 'h', render: (_, h) => <Text>{h.host}</Text> },
                      { title: '连接数', key: 'c', align: 'right', width: 90, render: (_, h) => h.conns },
                      { title: '字节', key: 'b', align: 'right', width: 110, render: (_, h) => fmtBytes(h.bytes) },
                      {
                        title: '最后兜底',
                        key: 'l',
                        width: 160,
                        render: (_, h) => <Text type="secondary">{fmtDateTime(h.lastTs)}</Text>,
                      },
                      {
                        title: '规则片段',
                        key: 'r',
                        width: 110,
                        render: (_, h) => (
                          <Button size="small" onClick={() => copyRules([ipRule(h.host)])}>
                            复制
                          </Button>
                        ),
                      },
                    ]}
                  />
                </Card>
              )}
            </>
          )
        }}
      </AsyncBody>
    </>
  )
}
