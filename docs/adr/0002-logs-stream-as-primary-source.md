# 以 /logs 文本流为主数据源，而非 /connections

需求是「尽量完整」地捕获一段时间内所有「明文 HTTP → 出境节点」的连接。mihomo 的 `/connections` 只返回**当前活跃**连接、关闭即删、无历史，而明文 HTTP 连接往往是亚秒级的（连上→请求→响应→关闭），快照 / 1 秒轮询会**系统性漏掉**。相比之下 `/logs` 在连接**建立时**就以 Info 级吐一行（形如 `[TCP] src --> host:port match RULE using NODE`），短连接照样记录，且这一行已含目的 `host:port`（→ 端口判 HTTP）、出境节点（→ 地区标签 + `DIRECT` 天然区分）、命中规则。

**消费方式**：mihomo 的 `/logs` 对普通 HTTP GET 即流式返回换行分隔的 JSON（逐条 flush），**无需 websocket**，故消费端零第三方依赖（标准库 `net/http` + `json.Decoder` 即可）。另已核对 mihomo `log` 包：日志事件无条件先入 `logCh` 再按全局 level 决定是否打到 stdout，因此 `/logs?level=info` 的订阅**与 mihomo 全局 `log-level` 设置无关**，均能收到 info 级连接日志，无需要求用户改 mihomo 配置。

因此主数据源选 `/logs` 流。代价：

- 日志是**非结构化文本**，解析随 mihomo 版本有变更风险（需针对格式做防御性解析 + 版本适配）
- 拿不到**字节数 / 进程 / GeoIP**（这些只在 `/connections` 有）
- 需要 mihomo **日志级别为 info**

若将来需要字节级统计，可再引入 `/connections` 做二级富化（届时另记 ADR）。
