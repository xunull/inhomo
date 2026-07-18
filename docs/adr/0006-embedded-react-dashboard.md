# 阶段3：内嵌 React 仪表盘（Vite + Ant Design + Recharts，go:embed）

阶段2 后端给出 3 个 JSON 接口。阶段3 做一个前端仪表盘，打进同一个二进制、由 `serve` 的 Fiber 托管。

## 决策

- **技术栈**：Vite + React + TypeScript 的 SPA（放 `web/`）；**Ant Design**（Layout/Card/Statistic/Table/Select 等组件）+ **Recharts**（条/线/环图）。
- **打包内嵌**：`npm run build` → `web/dist/`；Go 侧 `web/embed.go`（package web）`//go:embed all:dist` 把静态资源打进二进制；Fiber 用 filesystem 中间件托管，客户端路由回退 `index.html`。→ 仍是单文件二进制，`serve` 起来访问 `http://127.0.0.1:<addr>/`。
- **构建顺序（gotcha）**：`go build` 的 embed 需要 `web/dist/` 已存在，否则编译失败。用 Makefile/脚本先 `npm run build` 再 `go build`；仓库提交一个占位 `web/dist`（含极简 index.html）保证没前端产物时 `go build` 也过。
- **开发期**：Vite dev server（HMR）代理 `/api` → 运行中的 `serve` 后端；发布才 build+embed。
- **仪表盘（多面板）**：
  - 顶部 KPI 概要条（Statistic 卡）← `/api/summary`
  - 连接数时间曲线（Recharts 线/面积）← `/api/timeseries`
  - 面板并列（Card + 条形/表）：热门域名、App 画像、出境节点、地区、端口 ← `/api/aggregate?by=...`
  - 全局时间窗（Select：1h/24h/7d）驱动所有面板；自动刷新（轮询，因为 serve 在实时记）。
- **绑定/隐私**：沿用后端——只绑 `127.0.0.1`、无鉴权。

## 分期

阶段3 内部再拆：先「embed 骨架 + 一个面板跑通」，再逐个加面板/图/全局筛选/自动刷新。
