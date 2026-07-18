# 阶段2：serve 命令 + Fiber Web 分析接口

阶段1 的 `record` 把连接事件写入嵌入式 DuckDB。阶段2 在**同一进程**加一个 Web 服务查询该
DuckDB、对外出 JSON 分析接口（为阶段3 的 React 前端打底）。

## 决策

- **命令**：新增 `inhomo serve` = 记录 + Web（同进程，规避 DuckDB 单写锁）；`record` 保留为纯
  收集器（不开 Web）。serve 是 record 的超集。**同一个库文件只能被一个写进程持有**，故 `record`
  与 `serve` 不能同时对同一 `--db` 运行——用其一。
- **框架**：Fiber（Go，纯 Go 无 CGO；引入 fasthttp 等依赖）。
- **API 形态**：参数化聚合，而非固定策展或裸 SQL：
  - `GET /api/aggregate?by=<dim>&since=<dur>&limit=<n>`：按维度 top-N；`by` **白名单**限定到
    `host|process|node|region|port|rule`（防注入，尽管 localhost）。
  - `GET /api/summary`：总量与分布（总连接、去重 host/process/node、直连 vs 代理、HTTP vs HTTPS、时间跨度）。
  - `GET /api/timeseries?since=&bucket=`：按时间桶的连接数。
  - 时间过滤：`since` 用相对时长（Go `ParseDuration`，如 `24h`/`168h`），默认全量。
- **查询层**：SQL 封装在 `store` 的 Go 方法里（参数白名单 + 时间过滤），可对临时 DuckDB 测；
  Fiber handler 只调用 + 编码 JSON。
- **绑定/安全**：只绑 `127.0.0.1:<port>`（默认端口可配，如 `127.0.0.1:8464`），**无鉴权**——
  接口返回访问历史，数据不出本机（隐私优先）。
- **数据新鲜度**：查询经 `store.DB()` 与写入 appender 在同进程并发；appender 缓冲到 flush（默认 5s）
  才可见，故查询有 ≤5s 滞后，可接受。

## 分期

阶段2 只做**后端**（`curl` 可验）；阶段3 做 React 前端（`go:embed` 进二进制、Fiber 托管）。
