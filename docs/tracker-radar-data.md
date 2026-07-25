# 追踪器识别数据（`tracker-radar.json`）技术说明

`inhomo tracker update` 拉取的那个文件。本文说明它「是什么、从哪来、什么格式、怎么用、
为什么是运行期下载而非内嵌」，供理解 `audit` 标注、仪表盘「追踪器暴露」卡与 `report` 的数据来源。

> 数据源：DuckDuckGo **Tracker Radar** 的 `build-data/generated/domain_map.json`
> （`https://raw.githubusercontent.com/duckduckgo/tracker-radar` 的 `main` 分支）。
> 相关代码：`internal/tracker/tracker.go`（拉取 + 解析 + 归类）、`internal/cli/tracker.go`
> （`tracker update` 子命令）、`internal/cli/audit.go` / `serve.go`（消费方）。相关决策：ADR-0011。

## 1. 它是什么

一张 **「域名 → 归属公司」对照表**——DuckDuckGo Tracker Radar 收录的、跨全网观测到的
**已知追踪器 / 第三方请求域名**及其背后实体。落到本机后是单个 JSON 文件：

- **路径**：`~/.inhomo/tracker-radar.json`（与配置、连接库同目录）
- **体量**：约 10MB、**~38367 条**域名
- **每条**：一个已注册域名（eTLD+1）→ 它的归属公司

真实样例（截取）：

```json
{
  "google-analytics.com": { "entityName": "Google LLC", "displayName": "Google", "aliases": ["Alphabet Inc.", "Google Inc.", "Waze Mobile", "…"] },
  "doubleclick.net":       { "entityName": "Google LLC", "displayName": "Google" },
  "scorecardresearch.com": { "entityName": "comScore, Inc", "displayName": "comScore" }
}
```

inhomo 只用其中的 `displayName`（如 `Google`，空则回落 `entityName`）作为「归属公司」；
`aliases` 等其它字段忽略。**「在这张表里」≈「是已知的第三方/追踪器域名」**。

## 2. 从哪来、放哪、怎么更新

```bash
inhomo tracker update
```

该命令从上游 `domain_map.json` 下载，**先写临时文件再原子改名**到 `~/.inhomo/tracker-radar.json`
（避免半截文件），并打印写入条数。数据带版本快照性质——想更新就**再跑一次覆盖**；v0 不做自动刷新/陈旧提醒。

## 3. 怎么被使用（三个消费方，都靠它）

归类器（`internal/tracker`）把这张表**加载进内存**成一张查表。查一个连接的目的 `host` 时：
取它的 **[eTLD+1](./etld-plus-one.md)**（可注册域，用公共后缀把 `www.google-analytics.com` → `google-analytics.com`）在表里找——
命中 = 已知追踪器 + 归属公司；没命中（含 IP、单标签等取不到 eTLD+1 的）= 未知。**下载后全程离线查，不再联网。**

| 消费方 | 用途 |
|---|---|
| `inhomo audit` | 明文泄露行标注 `[已知追踪器 · Google]`（进程内查表，`audit` 是 `/logs` 实时流、不查 DuckDB） |
| `inhomo serve` 仪表盘 + `/api/trackers` | 「追踪器暴露」卡：多少连接走了已知追踪器 + top 归属公司；未拉取时卡上提示跑 `tracker update` |
| `inhomo report` | AI 周报的素材之一（上面那份「占比 + 归属」聚合） |

## 4. 为什么是运行期下载，而不是打进二进制

Tracker Radar 的**数据**许可是 **CC BY-NC-SA 4.0**（非商业 + 相同方式共享），
把它当 inhomo（Apache-2.0）二进制的一部分再分发站不住。所以不 `go:embed`，
改由 `tracker update` 运行期拉到你本机、离线比对——二进制保持纯 Apache、仓库不含版权数据。
详见 ADR-0011。

## 5. 隐私

- **查询不外发**：下载的是**整份清单**，之后查你访问的域名全在本地内存比对，不会逐个域名去联网——对隐私工具是关键。
- **下载不上传**：`tracker update` 只是从上游 GitHub **下载**这份公开数据，不上传你的任何连接/域名。

## 6. 没有它会怎样（优雅退化）

不下载 / 断网 / 文件损坏 → 归类器为空，一切照常跑，只是**不做追踪器标注**（全部算「未知」），**不报错、不阻塞**：

- `audit` 启动时提示一次「未加载追踪器数据（跑 `inhomo tracker update`…）」，之后照常审计；
- 仪表盘「追踪器暴露」卡显示明确的「未拉取追踪器数据」提示（`/api/trackers` 返回 `loaded:false`）；
- 取不到 eTLD+1 的 host（IP 字面量、单标签）恒归「未知」，绝不报错。

## 7. v0 范围与局限

- **只有「归属」，没有「类别」**：这张合并单文件只给归属公司，不含 advertising/analytics/CDN 等**细粒度类别**——类别只存在于 Tracker Radar 的**逐域名文件**里（几千个），单文件拉不到。故 v0 归类是「已知追踪器 + 归属」二元，未做多类目。
- **「不在表里」≠「可疑」**：表里装的是已知第三方/追踪器；你直连的一方站点（news、银行、github 等）本就不在表里、归「未知」，这很正常，不代表有问题。

## 8. 许可与署名

数据版权归 **DuckDuckGo Tracker Radar**，许可 **CC BY-NC-SA 4.0**。inhomo 不再分发它、仅在运行期从上游拉取到用户本机；使用/引用时请遵循该许可的署名与非商业条款。
