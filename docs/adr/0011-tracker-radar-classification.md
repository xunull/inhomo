# 追踪器识别：Tracker Radar 运行期拉取 + 进程内归类（归属公司，暂不细分类别）

inhomo 把连接的目的 `host` 全量记下，但审计泄露行只显示裸域名，用户分不清 `sb.scorecardresearch.com` 是不是追踪器、归谁。要把裸 host 变成「已知追踪器 + 归属公司」，让 `audit` 行从「暴露了这个域名」升级成「暴露了一个 comScore 的已知追踪器」。这是 office-hours 设计《AI 隐私分析师之「域名归类」》的第一个楔子（T41）。

## 决策

- **数据源 = DuckDuckGo Tracker Radar 的 `domain_map.json`**（域名 → 归属实体，约 3.8 万条、10MB）。它是唯一契合「逐域名 → 归属」的合并单文件。
- **运行期拉取，不内嵌（office-hours 敲定的路线 B）**：Tracker Radar 的**数据**许可是 **CC BY-NC-SA 4.0**（非商业 + 相同方式共享），打进 Apache-2.0 二进制再分发站不住。故不 `go:embed`，改由 `inhomo tracker update` 运行期从上游拉到 `~/.inhomo/tracker-radar.json`，之后**离线**比对。二进制保持纯 Apache，仓库不含版权数据。
- **进程内 Go 归类器**：用公共后缀（`golang.org/x/net/publicsuffix`）取 host 的 eTLD+1 查表得归属。**`audit` 是从 `/logs` 实时流出的、不查 DuckDB**，故标注走进程内查表，不是 SQL join。
- **v0 只做「已知追踪器 + 归属公司」，暂不细分类别**：`domain_map.json` 给归属、**不给类别**（advertising/analytics/social/cdn）；类别只存在于 Tracker Radar 的**逐域名文件**里（几千个），单文件拉不到。要类别得拉整仓 tarball（~100MB），代价大，留作后续（T42 再定要不要拉）。
- **优雅退化（硬约束）**：数据未拉取 / 断网 / 文件损坏 → 归类器为空、**全部 unknown**，泄露行不标注、**绝不报错阻塞**（守护/审计不因缺数据而挂）。取不到 eTLD+1 的 host（IP 字面量、单标签、空）→ unknown。

## 取舍

- **运行期拉取 vs 内嵌**：内嵌省一次网络、开箱即用，但会把 CC BY-NC-SA 数据塞进二进制、污染许可。选拉取——保住纯 Apache 二进制与「查域名不外泄」（拉的是整份清单，不是逐个查询），代价是**首次要跑 `inhomo tracker update`**、非纯自包含（`brew services` 常驻场景需先预热一次）。
- **归属 vs 类别**：单文件只给归属。先发「追踪器 + 归属」这一版（核心 whoa 已足），类别作为需要更重数据源的增量后置。这偏离了设计文档最初「5 类」的设想——是数据可得性驱动的诚实收缩，非放弃。
- **显式 `tracker update` vs audit 首跑自动拉**：选显式命令（像「更新病毒库」），避免在 audit 热路径里做意外网络请求；缺数据只提示、不自动联网。
- **新依赖 `golang.org/x/net`**：公共后缀取 eTLD+1 的标准库，体量小、来源正。

## 与 ADR / 设计的关系

本 ADR 落的是 office-hours 设计里 Approach B 的 T41 楔子的**具体选型**，与设计文档有两处按实况修正：① 许可路线由「内嵌 + 混合许可」改为用户拍板的**运行期拉取**；② 因合并单文件无类别，v0 归类降为「追踪器 + 归属」。不涉及 ADR-0009（配置）/ADR-0010（自动发现）的语义。

## v1 不做

细粒度类别（advertising/analytics/…，待 T42）；AI 残差归类（待 T43）；audit 首跑自动拉取；仪表盘 `by=category` 维度（T42）；Windows 缓存路径；数据集陈旧提醒/自动刷新。
