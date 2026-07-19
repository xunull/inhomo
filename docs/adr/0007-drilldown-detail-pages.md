# 数值钻取：过滤切片详情页 + react-router

主仪表盘只给聚合数字（KPI、top-N 条形），看不到「某个数字背后是哪些具体连接」。要能点数字/条形 → 钻入更详细的页面。

## 领域模型

- **钻取**的对象是**过滤切片**（见 CONTEXT）：连接事件的一个子集，由 `filter` 界定。
- `filter` = 精确列（`host/process/node/region/port`，白名单）+ 谓词（`route=direct|proxied`）+ `since`；**可叠加**。
- 数字与 filter 的映射（口径同 `store.Summary`）：`直连→route=direct`（`node='DIRECT'`）、`经代理→route=proxied`（`node<>'' AND node<>'DIRECT' AND node NOT LIKE 'REJECT%'`）、`HTTP·80→port=80`、`HTTPS·443→port=443`、`总连接→`空 filter。

## 决策

- **两种页型**：
  - **维度总览页** `/d/:dim`（dim ∈ host/process/node/region/port）：该维度全量排名（值+计数+占比，可搜索排序，不止 top-N），每行可钻入该取值的过滤详情。来源：去重域名/App/出境节点 KPI、面板标题。
  - **过滤详情页** `/detail?<filter>&since=`：对该过滤切片的**迷你仪表盘**（复用主仪表盘全套组件、按切片重算、**隐藏被精确过滤钉死的维度**）+ 面包屑 chips（可逐个删）+ **原始连接明细表**。来源：条形、过滤型 KPI、总览页某行。
- **路由**：引入 `react-router-dom`，filter 编码进 URL query（可分享、前进/后退、深链接；Fiber 已对任意路径回退 `index.html`）。
- **组件复用**：主仪表盘重构为**按 filter 参数化**的组件，主页 = 空切片、详情 = 带 filter 的同一组件。
- **后端**：抽一个共享**过滤器构造**（白名单列 + route 谓词 + since → WHERE 子句，参数化防注入）；`/api/aggregate` 与 `/api/timeseries` 各加这套过滤参数；新增 `/api/connections?<filter>&since=&offset=&limit=` → `{rows:[{ts,process,network,host,port,rule,node,region}], total}`，默认 `ts DESC`，每页 50、上限约 200。
- **详情页行为**：继承 `since`、自带时间窗选择器；**自动刷新默认关**（调查时不让明细行乱跳）。

## 取舍

- **可叠加过滤 vs 单层**：选可叠加（面包屑 chips），换来「HTTP 且某域名」这类交叉钻取的调查力，代价是前端过滤栈 UI + 后端接受一组约束。
- **react-router vs 手搓 History vs Drawer**：选 router，深链/前进后退/边缘情况开箱即用，代价一个成熟依赖；用户明确要「跳转到页面」而非弹层。
- **过滤后重算聚合 vs 只给明细表**：选重算（迷你仪表盘），信息一致、可继续钻，代价是 aggregate/timeseries 也要接过滤参数。

## v1 不做

明细表列排序 / 单元格点选加过滤 / 保存过滤 / rule 维度面板 / 明细导出。
