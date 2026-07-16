# inhomo

审计经由 mihomo（Clash Meta 内核）出站的流量，找出以「明文 HTTP」形式、且经由「不可信/境外节点」中转的访问，用于隐私泄露的持续监控与告警。

## Language

**明文 HTTP 连接**：
经由 mihomo 出站、目的端口属于 HTTP 端口集（默认 `80`）的一条 TCP 连接。观测的最小单位是「连接」，不是单个 HTTP 请求——TUN 模式下 mihomo 工作在 L4，`/connections` 拿不到 method / path / header；浏览器 keep-alive 复用的一条连接里的多个请求，只算一条。
_Avoid_: HTTP 请求、HTTP flow

**出境节点**：
一条连接实际经过的、`chains` 里最后一个真实代理节点。`DIRECT` / `REJECT` 不算出境节点（明文没有暴露给第三方中转，不构成泄露）。
_Avoid_: 代理、出口、proxy

**明文 HTTP 泄露事件**：
本项目要告警/记录的核心单位 = 一条「明文 HTTP 连接」且它「经过了出境节点」（`chains` 非 `DIRECT`/`REJECT`）。这才是告警条件，不是任意 HTTP、也不是任意代理连接。
_Avoid_: HTTP 告警、泄露连接

**节点地区标签**：
从「出境节点」名字里尽力解析出的国家/地区（靠国旗 emoji 或国家字样）；解析不出就记 `unknown`。只作分类/排序标签，不作硬性筛选条件——因为 `/connections` 与 `/proxies` 都拿不到节点服务器 IP，无法做权威 GeoIP。
_Avoid_: 节点国家、GeoIP
