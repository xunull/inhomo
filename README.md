# inhomo

**审计经由 mihomo 出站的明文 HTTP 泄露，并把全量连接事件落进嵌入式 DuckDB，配一个内嵌的 React 仪表盘做分析。**

inhomo 订阅 [mihomo](https://github.com/MetaCubeX/mihomo)（Clash Meta 内核）的 `/logs` 流，逐条解析连接日志，做两件事：

1. **审计明文 HTTP 泄露** —— 找出以明文 HTTP（默认目的端口 `80`）形式、且经由**境外/不可信代理节点**中转的访问。这类流量的内容对中转节点是可见的，是隐私泄露面。
2. **全量连接分析** —— 把每一条连接事件（不止泄露）写入嵌入式 DuckDB，通过一个同进程的 Web 服务 + React 仪表盘做聚合分析（热门域名、App 画像、出境节点占比、地区分布、时间曲线……）。

整个程序是**单二进制**：CLI、Web 服务、DuckDB、前端仪表盘全部打包在一个可执行文件里，本机运行、数据不出机。

---

## 目录

- [它解决什么问题](#它解决什么问题)
- [工作原理](#工作原理)
- [环境要求](#环境要求)
- [通过 Homebrew 安装](#通过-homebrew-安装)
- [从源码安装与构建](#从源码安装与构建)
- [快速开始](#快速开始)
- [命令参考](#命令参考)
- [配置](#配置)
- [后台常驻](#后台常驻)
- [Web 分析接口](#web-分析接口)
- [数据模型](#数据模型)
- [连接到 mihomo](#连接到-mihomo)
- [核心概念](#核心概念)
- [设计决策](#设计决策)
- [项目结构](#项目结构)
- [隐私与并发约束](#隐私与并发约束)
- [开发](#开发)

---

## 它解决什么问题

在 TUN（虚拟网卡）模式下，本机所有流量都经 mihomo 出站。其中一部分是**明文 HTTP**：URL、Host、甚至 body 都没有 TLS 保护。如果这样一条连接又被分流到了**境外代理节点**，那么内容对该节点运营者就是可读的——这是一条真实的隐私泄露路径，且平时完全无感。

inhomo 把这条路径显式地监控起来：告警条件不是「任意 HTTP」也不是「任意代理连接」，而是二者的交集——**明文 HTTP 连接 且 经过了出境节点**（`chains` 非 `DIRECT`/`REJECT`）。同时它顺手把全量连接留档，让你能回看「哪些 App、访问了哪些域名、走了哪些节点、去了哪些地区」。

## 工作原理

mihomo 的 `/logs` 是一个**纯 HTTP GET 流**（逐行 newline 分隔的 JSON，无需 WebSocket）。每条 `[TCP]` 日志由 mihomo 在**连接建立那一刻只打一行**，因此 inhomo 的观测最小单位是「一条 TCP 连接」，既不是单个 HTTP 请求，也不是数据包。

```
mihomo  ──GET /logs?level=info（逐行 JSON 流）──►  inhomo
                                                    │
                                        detect.Parse（解析每条 [TCP] 连接日志）
                                                    │
                        ┌───────────────────────────┼───────────────────────────┐
                        ▼                            ▼                            ▼
                  audit：明文HTTP           record：全量连接事件          logs：原样逐行
                  + 出境节点 → 告警           → 嵌入式 DuckDB              打印 mihomo 日志
                  （终端 / JSONL）                   │
                                                    ▼
                                          serve：同进程 Web 服务
                                          ├─ React 仪表盘（/）
                                          └─ 分析接口（/api/*）
```

一条连接日志形如：

```
[TCP] 192.168.1.2:54321(curl) --> example.com:80 [match RuleSet | GEOIP] using 🚀 节点选择[🇺🇸美国HY2-06|1.0X]
```

inhomo 从中解析出：进程（`curl`）、目的 host/端口（`example.com:80`）、命中规则、出境节点（取 `chains` 里最后一个真实节点）、以及从节点名推断的地区（`🇺🇸` → `US`）。

## 环境要求

- **Go 1.26+**
- **C 编译器 + CGO** —— DuckDB 通过 [go-duckdb](https://github.com/marcboeker/go-duckdb) 以 CGO 方式嵌入，构建 `record`/`serve` 需要 `CGO_ENABLED=1`（macOS 自带 clang，Linux 需 gcc/clang）。
- **Node.js**（仅当你要**重新构建前端**时需要）—— 前端 `web/dist` 已提交进库，裸 `go build` 无需 Node 即可内嵌它。
- **一个在运行的 mihomo**，且开启了 `external-controller`（TCP 端口或 Unix socket）。

## 通过 Homebrew 安装

预编译二进制（macOS / Linux，各 arm64 + amd64）经 Homebrew tap 分发，装完即用、无需 Go 工具链：

```bash
brew tap xunull/tap       # tap 仓库：github.com/xunull/homebrew-tap
brew install inhomo       # 或一步到位：brew install xunull/tap/inhomo
```

升级 `brew upgrade inhomo`，卸载 `brew uninstall inhomo`。装完仍需一个在运行的 mihomo（见下）。

> **关于「未签名」**：二进制未做 Apple 公证，但**经 Homebrew 安装无需手动放行**——Homebrew 用自带 curl 下载，不会给文件打 `com.apple.quarantine` 隔离标记（那是浏览器下载才加的），所以装完可直接运行。仅当你绕过 brew、用浏览器直接下 Release 里的 `tar.gz` 时，才需 `xattr -d com.apple.quarantine ./inhomo` 放行。

## 从源码安装与构建

需 Go + CGO（见上「[环境要求](#环境要求)」）；改代码或无 Homebrew 时用这条路径。

```bash
# 克隆后，一条命令构建前端 + 内嵌 + go build，产出单二进制 ./inhomo
make

# 若要改前端，先装依赖，再单独构建前端
make deps        # npm install（首次或 package.json 变更后）
make frontend    # 构建 web/dist（go:embed 需要它）

# 只构建 Go（用已提交的 web/dist，无需 Node）
make build       # 等价于 CGO_ENABLED=1 go build -o inhomo .

# 全套 Go 测试
make test
```

## 快速开始

三步看到仪表盘（假设你的 mihomo controller 在默认的 `127.0.0.1:9090`）：

```bash
# 1) 构建
make

# 2) 一边记录连接、一边开 Web 服务（默认库 ~/.inhomo/connections.duckdb）
./inhomo serve --controller 127.0.0.1:9090 --secret <你的secret>

# 3) 浏览器打开
open http://127.0.0.1:8464/
```

产生一些流量（正常上网即可），仪表盘上就会出现 KPI、Top 域名/节点、时间曲线。

只想快速验证连通、看看 mihomo 在打什么日志：

```bash
./inhomo logs --controller 127.0.0.1:9090 --secret <你的secret>
```

只想审计明文 HTTP 泄露（不落库、只在终端冒泄露事件）：

```bash
./inhomo audit --controller 127.0.0.1:9090 --secret <你的secret>
```

## 命令参考

所有命令共享两个 root 持久 flag：

| Flag | 默认 | 说明 |
|---|---|---|
| `--controller` | `127.0.0.1:9090` | mihomo external-controller。TCP 如 `127.0.0.1:9090`，或 Unix socket 如 `unix:///tmp/verge/verge-mihomo.sock` |
| `--secret` | `""` | external-controller 的 secret（未开启鉴权则留空） |

### `inhomo audit`

识别并记录「明文 HTTP + 经出境节点」的泄露事件。终端按 `(出境节点, 目的host)` 时间窗去重、不刷屏；可选把每一条原始事件追加写 JSONL。

| Flag | 默认 | 说明 |
|---|---|---|
| `--level` | `info` | 订阅的日志级别；连接日志需要 `info` |
| `--http-ports` | `80` | 视为明文 HTTP 的目的端口集（逗号分隔，如 `80,8080`） |
| `--out` | `""` | JSONL 输出文件路径（留空=只打印终端、不落盘） |
| `--window` | `5m` | 终端聚合时间窗：同 `(节点,host)` 在窗内只冒一次 |

终端输出示例：

```
15:04:05  明文HTTP泄露  example.com:80  →  🇺🇸美国HY2-06|1.0X [US]  规则:GEOIP  (过去 5m0s 内又 ×3)
```

### `inhomo logs`

原样查看 mihomo 日志（逐行 payload + 级别标记），用于排查连通性或直接观察内核在打什么。

| Flag | 默认 | 说明 |
|---|---|---|
| `--level` | `info` | 订阅的日志级别：`info` / `warning` / `error` / `debug` |

### `inhomo record`

把每条连接事件（**全量，不止泄露**）写入嵌入式 DuckDB，供后续分析统计。

| Flag | 默认 | 说明 |
|---|---|---|
| `--level` | `info` | 订阅的日志级别；连接日志需要 `info` |
| `--db` | `~/.inhomo/connections.duckdb` | DuckDB 库文件路径。空=默认路径；`~/` 前缀会展开到 home，目录不存在自动创建 |

### `inhomo serve`

`record` 的超集：同进程一边记录连接、一边开 Web 服务，托管 React 仪表盘与分析接口。

| Flag | 默认 | 说明 |
|---|---|---|
| `--level` | `info` | 同 `record` |
| `--db` | `~/.inhomo/connections.duckdb` | 同 `record` |
| `--addr` | `127.0.0.1:8464` | Web 监听地址（默认仅本机、无鉴权；填非回环地址会打印警告） |

> 记录后台跑；一旦记录侧断开（如 `/logs` 连接失败），会连带关闭 Web，避免「记录已死、Web 空转」。

## 配置

除命令行 flag 外，上述参数也可写进 **`~/.inhomo/config.yaml`** 或用 **`INHOMO_*` 环境变量**提供。三者优先级：

> **显式 flag > 环境变量 > 配置文件 > 内置默认**

[后台常驻](#后台常驻)（`brew services`）时尤其有用：把 controller 写进配置，服务就无需在命令行反复传 `--controller unix://…`。文件不存在按默认走（不报错）；文件存在但格式错会报错。详见 ADR-0009。

`~/.inhomo/config.yaml` 示例（键名同 flag 名，`serve`/`record`/`audit`/`logs` 共享一份，各取所需）：

```yaml
controller: unix:///tmp/verge/verge-mihomo.sock
secret: ""
db: ~/.inhomo/connections.duckdb
traffic-interval: 3s
addr: 127.0.0.1:8464
http-ports: "80"
window: 5m
```

环境变量 = `INHOMO_` 前缀 + 键名大写、连字符换下划线：

```bash
export INHOMO_CONTROLLER=unix:///tmp/verge/verge-mihomo.sock
export INHOMO_TRAFFIC_INTERVAL=0   # 关闭流量采集
```

## 后台常驻

经 Homebrew 安装的 inhomo 自带 service 定义，用 `brew services` 即可让它后台常驻、登录/开机自启（mac 走 launchd、linux 走 systemd，一份定义两边覆盖）：

```bash
brew services start inhomo     # 起后台 serve，并登录/开机自启
brew services stop inhomo      # 停
brew services restart inhomo   # 改了 config.yaml 后重启生效
brew services info inhomo      # 看状态
```

常驻服务跑的是 `inhomo serve` 且**不带任何命令行参数**——controller / secret / db / addr 全从 [`~/.inhomo/config.yaml`](#配置) 读。所以起服务前先把配置写好，尤其 unix-socket 形态的 mihomo：

```yaml
# ~/.inhomo/config.yaml
controller: unix:///tmp/verge/verge-mihomo.sock
secret: ""
```

> **别加 `sudo`**：`brew services start inhomo`（不带 sudo）以你的用户身份跑，`$HOME` 才是你的家目录，才能找到 `~/.inhomo/config.yaml` 和默认库 `~/.inhomo/connections.duckdb`。加 sudo 会以 root 跑、读不到你的配置。
>
> **`INHOMO_*` 环境变量对守护进程无效**：launchd/systemd 不加载你的 shell（不读 `~/.zshrc`），故常驻场景请一律用 config.yaml，别指望 env。
>
> **别和手动 `serve` 抢库**：服务已在写默认库，就别再手动 `inhomo serve`/`record` 开同一个库（DuckDB 单写锁，见「[隐私与并发约束](#隐私与并发约束)」）。
>
> 服务日志写在 `$(brew --prefix)/var/log/inhomo.log`；仪表盘照常在 `http://127.0.0.1:8464/`。

## Web 分析接口

`serve` 提供以下接口（同源，前端也由它托管）。未知的 `/api/*` 返回 404 JSON，其余未匹配路由回退到仪表盘 `index.html`（SPA）。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/` | React 仪表盘 |
| GET | `/api/summary` | KPI 概要 |
| GET | `/api/aggregate?by=&since=&limit=` | 按维度的 top-N |
| GET | `/api/timeseries?since=&bucket=` | 按时间桶的连接数 |

**时间参数格式**：`since` / `bucket` 支持 Go 时长（`24h`、`90m`）或 `7d`（天）；`since` 留空表示「全部时间」。

**`by` 维度白名单**：`host` / `process` / `node` / `region` / `port`（其它值返回 400）。

示例：

```bash
curl 'http://127.0.0.1:8464/api/summary'
# {"total":41,"hosts":22,"processes":7,"nodes":2,"direct":4,"proxied":37,
#  "http":11,"https":30,"earliest":"2026-07-18T14:19:44Z","latest":"2026-07-18T14:19:56Z"}

curl 'http://127.0.0.1:8464/api/aggregate?by=host&since=24h&limit=5'
# [{"key":"api.github.com","count":16},{"key":"chatgpt.com","count":3}, ...]

curl 'http://127.0.0.1:8464/api/timeseries?since=1h&bucket=5m'
# [{"ts":"2026-07-18T14:15:00Z","count":41}]
```

`summary` 字段口径：`proxied` = 经出境节点（`node` 非空、非 `DIRECT`、非 `REJECT*`）；`direct` = 直连；`http`/`https` = 目的端口 80/443 的连接数。

## 数据模型

DuckDB 里只有一张表 `connections`，每条连接一行：

| 列 | 类型 | 含义 |
|---|---|---|
| `ts` | TIMESTAMP | 连接建立时刻 |
| `process` | VARCHAR | 发起连接的进程名（可能为空） |
| `network` | VARCHAR | 网络类型（如 TCP） |
| `host` | VARCHAR | 目的 host |
| `port` | INTEGER | 目的端口 |
| `rule` | VARCHAR | mihomo 命中的分流规则 |
| `node` | VARCHAR | 出境节点（`chains` 最后一个真实节点；直连为 `DIRECT`） |
| `region` | VARCHAR | 从节点名推断的地区（国旗 emoji / 国家字样；推断不出为 `unknown`） |

「明文 HTTP 泄露」只是这张表的一个过滤视图；仪表盘的各个面板则是它的不同 `GROUP BY`。库文件可直接用 `duckdb` CLI 打开做 ad-hoc 查询。

## 连接到 mihomo

inhomo 需要 mihomo 的 `external-controller`。两种形态都支持：

- **TCP**（多数 clash 配置）：`--controller 127.0.0.1:9090`
- **Unix socket**（如 Clash Verge Rev）：`--controller unix:///tmp/verge/verge-mihomo.sock`

若 controller 配了 `secret`，用 `--secret <值>` 传入。

> Clash Verge Rev 默认只暴露 Unix socket（`external-controller` 为空），socket 路径通常是 `/tmp/verge/verge-mihomo.sock`。

## 核心概念

术语的精确定义见 [`CONTEXT.md`](./CONTEXT.md)：

- **明文 HTTP 连接** —— 经 mihomo 出站、目的端口属 HTTP 端口集的一条 TCP 连接（最小单位是「连接」，不是请求/数据包）。
- **出境节点** —— `chains` 里最后一个真实代理节点；`DIRECT`/`REJECT` 不算。
- **明文 HTTP 泄露事件** —— 告警核心单位 = 明文 HTTP 连接 **且** 经过出境节点。
- **连接事件** —— 从 `/logs` 解析出的一条连接的结构化记录（全量），是 `record`/分析的基本单位。
- **节点地区标签** —— 从节点名尽力解析的国家/地区，仅作分类标签，不作硬筛选（拿不到节点 IP，无法权威 GeoIP）。

## 设计决策

关键取舍记录在 [`docs/adr/`](./docs/adr/)：

| ADR | 决策 |
|---|---|
| [0001](./docs/adr/0001-node-region-by-name-not-geoip.md) | 地区按节点名解析，而非 GeoIP |
| [0002](./docs/adr/0002-logs-stream-as-primary-source.md) | 以 `/logs` 流为主数据源（而非 `/connections` 快照） |
| [0003](./docs/adr/0003-adopt-cobra-cli.md) | 采用 cobra 组织子命令 |
| [0004](./docs/adr/0004-analytics-app-embed-duckdb.md) | 连接分析应用方向 + 嵌入式 DuckDB（CGO） |
| [0005](./docs/adr/0005-serve-fiber-web-api.md) | `serve` 命令 + Fiber Web 分析接口 |
| [0006](./docs/adr/0006-embedded-react-dashboard.md) | 内嵌 React 仪表盘（Vite + Antd + Recharts，go:embed） |

关于 mihomo `/logs` 的技术细节（格式、级别、投递语义、留存）见 [`docs/mihomo-logs.md`](./docs/mihomo-logs.md)。

## 项目结构

```
main.go                 入口（cli.Execute）
internal/
  cli/                  cobra 子命令：audit / logs / record / serve
  logstream/            /logs 流客户端（TCP + Unix socket，断线重连）
  detect/               连接日志解析 + 明文HTTP泄露分类 + 地区推断
  aggregate/            audit 的 (节点,host) 时间窗去重
  sink/                 JSONL 落盘
  store/                嵌入式 DuckDB：写入（Appender）+ 查询（summary/aggregate/timeseries）
web/                    Vite + React + TS 仪表盘；embed.go 用 go:embed 打包 dist
docs/adr/               架构决策记录
CONTEXT.md              领域术语表
Makefile                make = 前端 + 内嵌 + go build
```

## 隐私与并发约束

- **默认只绑本机、无鉴权**：`serve` 默认监听 `127.0.0.1:8464`。你的访问历史是敏感数据，不要把 `--addr` 改成非回环地址暴露到网络（会打印警告）。
- **DuckDB 单写锁**：一个 `.duckdb` 文件同一时刻只能被一个进程写。因此**不要对同一个库同时跑 `record` 和 `serve`**（`serve` 已包含记录）。若另一个 inhomo 进程正锁着默认库，再开一个会报 `Conflicting lock` —— 换一个 `--db` 路径，或先停掉那个进程。
- **数据不出机**：全部本地存储、本地查询，没有任何外发。

## 开发

```bash
make test                       # 全套 Go 测试（CGO）
go test ./internal/detect/...   # 单包
npm --prefix web run build      # 单独构建前端（tsc + vite）
```

前端改动后记得 `make frontend` 重建 `web/dist` 并提交——`go:embed` 内嵌的是已提交的构建产物。
