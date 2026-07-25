import { useMemo, useState } from 'react'
import { useSearchParams, useNavigate, Link } from 'react-router'
import { Alert, Button, Card, Collapse, Divider, Select, Space, Table, Tag, Typography } from 'antd'
import {
  getNewChannels,
  detailPath,
  NEW_WINDOWS,
  type Coverage,
  type NewAppGroup,
  type NewChannel,
} from '../api'
import { useApi } from '../useApi'
import { fmtDateTime, fmtTimeShort } from '../format'
import AsyncBody from './AsyncBody'

const { Text, Title } = Typography

// CoverageBanner：把「观测覆盖」摆在结论之前——「首次出现」只在确实有记录的时间里成立，
// 记录中断期间出现过的通道，恢复记录后会被误报为「新」（见 CONTEXT 术语、ADR-0014）。
function CoverageBanner({ cov }: { cov: Coverage }) {
  if (cov.earliest === null) return null
  const days = Math.round((cov.coveredHours / 24) * 10) / 10
  const summary = `基线自 ${fmtDateTime(cov.earliest)} 起，累计观测 ${cov.coveredHours} 小时（约 ${days} 天）`

  if (cov.gaps.length === 0) {
    return <Alert type="success" showIcon message={summary} description="期间没有记录中断，「首次出现」的判断可信。" />
  }
  // 空洞按时长降序，只列最长的三个——其余给个总数，避免刷屏。
  const worst = [...cov.gaps].sort((a, b) => b.hours - a.hours).slice(0, 3)
  return (
    <Alert
      type="warning"
      showIcon
      message={`${summary}，其中有 ${cov.gaps.length} 段没在记录`}
      description={
        <>
          <div>中断期间发生过的连接没有被观测到，因此紧随其后的「新增」里会混有并非真正首次出现的通道：</div>
          <ul style={{ margin: '6px 0 0', paddingLeft: 20 }}>
            {worst.map((g) => (
              <li key={g.start}>
                {fmtDateTime(g.start)} → {fmtDateTime(g.end)}，中断约 {g.hours} 小时
              </li>
            ))}
            {cov.gaps.length > worst.length && <li>……另有 {cov.gaps.length - worst.length} 段较短的中断</li>}
          </ul>
        </>
      }
    />
  )
}

// ChannelTable：一个 App 名下的新增通道明细。徽章只标不滤（见 ADR-0014）。
function ChannelTable({ process, channels, since }: { process: string; channels: NewChannel[]; since: string }) {
  const navigate = useNavigate()
  return (
    <Table<NewChannel>
      size="small"
      rowKey="host"
      dataSource={channels}
      pagination={channels.length > 20 ? { pageSize: 20, size: 'small' } : false}
      // 钻取走路由跳转（非 location.href）：整页重载会丢掉 SPA 状态、闪白屏。
      onRow={(r) => ({
        onClick: () => navigate(detailPath({ process, host: r.host }, since)),
        style: { cursor: 'pointer' },
      })}
      columns={[
        {
          title: '目的域名',
          key: 'host',
          render: (_: unknown, r: NewChannel) => <Text style={{ wordBreak: 'break-all' }}>{r.host}</Text>,
        },
        {
          title: '首次出现',
          key: 'firstTs',
          width: 110,
          render: (_: unknown, r: NewChannel) => <Text type="secondary">{fmtTimeShort(r.firstTs)}</Text>,
        },
        {
          title: '连接数',
          key: 'count',
          align: 'right' as const,
          width: 80,
          render: (_: unknown, r: NewChannel) => <Text type="secondary">{r.count}</Text>,
        },
        {
          title: '标记',
          key: 'badges',
          width: 260,
          render: (_: unknown, r: NewChannel) => (
            <Space size={4} wrap>
              {r.proxied && <Tag color="orange">经出境节点</Tag>}
              {r.plaintext && <Tag color="red">明文 80</Tag>}
              {r.tracker && <Tag color="purple">追踪器 · {r.tracker}</Tag>}
            </Space>
          ),
        },
      ]}
    />
  )
}

// appPanel：把一个 App 组渲染成折叠面板项；标题给出新增数与「经出境节点」条数的速览。
function appPanel(g: NewAppGroup, since: string) {
  const proxied = g.channels.filter((c) => c.proxied).length
  const trackers = g.channels.filter((c) => c.tracker).length
  return {
    key: g.process,
    label: (
      <Space wrap>
        <Text strong>{g.process}</Text>
        <Text type="secondary">{g.count} 个新域名</Text>
        {proxied > 0 && <Tag color="orange">{proxied} 经出境节点</Tag>}
        {trackers > 0 && <Tag color="purple">{trackers} 追踪器</Tag>}
      </Space>
    ),
    children: <ChannelTable process={g.process} channels={g.channels} since={since} />,
  }
}

// NewPage：/new 路由——「谁开始联系新地方」。
// 自带时间窗（默认 24h），**不跟随主页**：实测 1h→14 条、24h→254、7d→2745，跨两个数量级。
// 不接受过滤切片：首次出现要拿窗口外的历史当参照系（见 CONTEXT「过滤切片」边界）。
export default function NewPage() {
  const [params, setParams] = useSearchParams()
  const [refreshKey, setRefreshKey] = useState(0)
  const since = params.get('since') || '24h'
  const validSince = useMemo(
    () => (NEW_WINDOWS.some((w) => w.value === since) ? since : '24h'),
    [since],
  )
  const state = useApi(() => getNewChannels(validSince), [validSince, refreshKey])

  const setSince = (v: string) => {
    const next = new URLSearchParams(params)
    next.set('since', v)
    setParams(next, { replace: true })
  }

  return (
    <>
      <Space wrap align="center" style={{ marginBottom: 16 }}>
        <Link to="/">← 仪表盘</Link>
        <Text type="secondary">/ 新增</Text>
        <Text type="secondary">时间窗</Text>
        <Select value={validSince} onChange={setSince} options={NEW_WINDOWS} style={{ width: 130 }} />
        <Button onClick={() => setRefreshKey((k) => k + 1)}>立即刷新</Button>
      </Space>

      <Title level={5} style={{ marginTop: 0 }}>
        谁开始联系新地方
      </Title>
      <Text type="secondary">
        下列「应用通道」在此窗口内<Text strong>首次</Text>出现——该 App 以前从未联系过这个域名。
      </Text>

      <div style={{ marginTop: 16 }}>
        <AsyncBody
          state={state}
          skeletonRows={6}
          isEmpty={(d) => d.apps.length === 0}
          emptyText="该窗口内没有新增的应用通道"
        >
          {(data) => {
            const active = data.apps.filter((g) => !g.muted)
            const muted = data.apps.filter((g) => g.muted)
            return (
              <>
                <div style={{ marginBottom: 16 }}>
                  <CoverageBanner cov={data.coverage} />
                </div>
                {data.truncated && (
                  <Alert
                    type="info"
                    showIcon
                    style={{ marginBottom: 16 }}
                    message="结果已达条数上限，下方并非该窗口的全部新增——换更短的时间窗可看全。"
                  />
                )}

                {active.length > 0 ? (
                  <Collapse
                    items={active.map((g) => appPanel(g, validSince))}
                    defaultActiveKey={active.slice(0, 3).map((g) => g.process)}
                  />
                ) : (
                  <Card size="small">
                    <Text type="secondary">该窗口内没有后台进程的新增——新域名全部来自你自己在浏览器里点开的页面。</Text>
                  </Card>
                )}

                {muted.length > 0 && (
                  <>
                    {/* antd v6：文字位置是 titlePlacement，orientation 已改指分割线自身方向。 */}
                    <Divider titlePlacement="start" style={{ marginTop: 24 }}>
                      <Text type="secondary">以下是你自己指定目的地的进程（默认折叠）</Text>
                    </Divider>
                    <Text type="secondary" style={{ fontSize: 12, display: 'block', marginBottom: 8 }}>
                      这些进程的新域名是你自己点出来的，不构成「背着我」的证据。要改这份名单，用{' '}
                      <code>--mute-processes</code> 或配置文件里的 <code>mute-processes</code>。
                    </Text>
                    <Collapse items={muted.map((g) => appPanel(g, validSince))} />
                  </>
                )}
              </>
            )
          }}
        </AsyncBody>
      </div>
    </>
  )
}
