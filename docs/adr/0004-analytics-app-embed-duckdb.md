# inhomo 从 CLI 工具演进为连接分析应用（嵌入 DuckDB + 后续 Web UI）

在「明文 HTTP 泄露审计」之外，要对 mihomo 连接日志做通用的分析/统计——热门目的地、App/进程画像、
节点/地区/路由分布。这些本质是**同一条「连接事件」流的不同 group-by**。为此 inhomo 的定位从
「CLI 工具」扩展为「连接分析应用」：

- **记录**：`record` 订阅 `/logs`，把**全量连接事件**（不止泄露）写入**嵌入式 DuckDB**（`.duckdb` 文件）。
- **分析（后续阶段）**：**同一进程**内跑 **Fiber** Web 服务查询该 DuckDB，并用 `go:embed` 打包一个
  **React** 前端做可视化/聚合。

## 为什么同进程

DuckDB 一个 `.duckdb` 文件同时只允许一个读写进程。若「后台 record 常驻」与「另起进程 report」分离，
后者会被锁死。让**记录与查询在同一进程**（record 守护 + Fiber 查询同一 DuckDB 连接）即规避此锁。

## 关键取舍：放弃纯 Go

DuckDB 的 Go 驱动（`duckdb/duckdb-go`）是 **CGO**：需 C 编译器、把 DuckDB 预编译大库静态链入、
交叉编译要配 C 工具链 → 二进制显著变大、跨平台变复杂。这是继 ADR-0003（cobra）之后更进一步的
反转（此前核心 `detect`/`logstream`/`sink`/`aggregate` 仍纯 Go）。选它是为了「**单文件自带全功率
SQL 分析 + 内嵌 Web UI**」，且主要在本机 macOS 使用，代价可接受。

## 分期

**第一步只做「记录器」**（`record` → DuckDB 落全量连接事件），把记录做扎实；Fiber Web 与 React
前端为后续阶段。审计能力（`audit`）与原样查看（`logs`）保持不变。
