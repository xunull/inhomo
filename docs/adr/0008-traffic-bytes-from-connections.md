# 历史流量分析：接 /connections 拿字节（独立 traffic 数据集）

inhomo 一直只读 `/logs`（连接建立），只有连接数、没有流量字节，答不了「哪个 App/域名最耗带宽、谁在偷偷上传」。mihomo 的 `/connections` 能给每条**活跃**连接的累计上/下行字节 + 时长，接上它补足「流量」维度。

## 背景事实（实测 /connections）

`{downloadTotal, uploadTotal, connections[], memory}`。每条连接：`id`(UUID)、`upload`/`download`(该连接累计字节)、`start`、`chains`、`rule`、`metadata`(network/host/process/destPort/**destinationGeoIP**/ASN…)。它是**活跃连接的快照**，靠 `id` 追踪，连接一关就从列表消失。

## 决策

- **历史而非实时**：做「过去 N 时间谁最耗流量」的历史分析（记录已完成连接的字节），不做实时速率监控（留后续）。
- **采集**：record/serve 进程新起一条**轮询 `/connections`（每 ~3s GET）**的管线，与 `/logs` 流并行、同进程写 DuckDB。按 `id` 追踪累计字节；某 id 从快照消失（即「已完成连接」）→ 记下其最终上/下行字节 + 时长 + 元数据。
- **存储：新 `traffic` 表（独立数据集）**：`traffic(start_ts, process, network, host, port, node, region, up_bytes, down_bytes, duration_ms)`。现有 `connections`（`/logs` 来、全量计数）**不动**。地区沿用节点名解析（与现有一致）。
- **后端 `/api/traffic`**：复用 `store.Filter`（traffic 表同样有 host/process/node/region/port）；按维度的上/下行 top-N + 总上/下行；度量：上行/下行/合计。
- **前端：独立「流量」视图 `/traffic`**：上/下行 top-N 面板 + 总量；复用过滤切片 + 钻取；Dashboard 工具栏加入口（同「流量拓扑」）。

## 取舍

- **两套数据集（connections 全量计数 vs traffic 抽样字节）而非合并**：`/logs` 与 `/connections` 粒度不同、无稳定共享 key，硬对齐不靠谱；同一连接会被两边各记一次 → 合表会重复+脏。分开各管各的完整度，分析时按度量选表。
- **接受短连接漏字节**：开+关都在两次轮询之间的连接从没进过快照 → 无字节。这正是 ADR-0002 选 `/logs` 做主源的原因；但对带宽分析无妨——带宽被长连接主导，短连接本就没传多少。
- **轮询 GET 而非 WS 流**：`/connections` 有 ws 流变体，但周期 GET 最简、复用现有 controller/secret，自用够。
- **~3s 轮询**：更短抓更多短连接、更细，但开销更大；~3s 是平衡点。

## v1 不做

实时带宽监控；度量切换融进现有面板（连接数↔字节）；`destinationGeoIP` 真·目的地地区（现用节点名地区）；「流量」视图 UNION 当前活跃快照以补长活连接；WS 流。
