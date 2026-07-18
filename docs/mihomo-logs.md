# mihomo 日志（`/logs`）技术说明

inhomo 的数据来源。本文说明 mihomo 的连接日志「长什么样、怎么来、一行代表什么」，
供理解 `audit` / `logs` 两个子命令的语义，以及日后 mihomo 版本升级时排查格式漂移。

> 核对基准：mihomo `Meta` 分支源码；真机实测版本 `v1.19.25`（Clash Verge Rev）。
> 相关源码：`hub/route/server.go`（`/logs` 路由）、`log/log.go`（日志分发）、
> `tunnel/tunnel.go`（连接日志 `logMetadata`）、`constant/metadata.go`（字段）。

## 1. 我们消费的是 API 流，不是日志文件

inhomo **不读磁盘上的任何日志文件**，而是订阅 mihomo external-controller 的 **`/logs` 接口**。
它需要 mihomo 开着 external-controller（TCP 如 `127.0.0.1:9090`，或 Unix socket 如
`/tmp/verge/verge-mihomo.sock`），可能带 `secret` 鉴权（`Authorization: Bearer <secret>`）。

## 2. 传输与信封

`/logs` 普通 HTTP GET 即**流式返回**（无需 websocket），逐条 flush、**换行分隔的 JSON**：

```json
{"type":"info","payload":"[TCP] 198.18.0.1:55086(codex) --> chatgpt.com:443 match DomainSuffix(chatgpt.com) using 🚀 节点选择[🇺🇸美国HY2-06|1.0X]"}
```

- `type`：日志级别（`info` / `warning` / `error` / `debug` …）。
- `payload`：**日志正文**，就是下面要解析的那一行。**信封本身不含时间戳**（时间戳是 mihomo 自己控制台加的，API 不给）。
- `?level=info`：按级别过滤。**与 mihomo 全局 `log-level` 无关**——事件先无条件进 `logCh`（观察管道），全局 level 只决定是否再打到 stdout（见 `log/log.go`）。所以订阅 `info` 一定收得到 info 级连接日志，无需用户改配置。

## 3. 连接日志行的格式

TCP 连接日志（`payload`）由 `tunnel/tunnel.go` 的 `logMetadata` 产出，形如：

```
[TCP] <源地址>(<进程>) --> <目的host:port> [match <规则> | doesn't match any rule] using <出境节点>
```

常见变体：

| 变体 | 示例 |
|------|------|
| 规则命中 | `[TCP] 198.18.0.1:55086(codex) --> chatgpt.com:443 match DomainSuffix(chatgpt.com) using 🚀 节点选择[🇺🇸美国HY2-06\|1.0X]` |
| 直连 | `[TCP] mihomo --> 223.6.6.6:443 match GeoIP(cn) using 🎯 全球直连[DIRECT]` |
| 全局模式 | `[TCP] <src> --> <host:port> using GLOBAL` |
| 未命中规则 | `[TCP] <src> --> <host:port> doesn't match any rule using <node>` |
| specialProxy | `[TCP] <src> --> <host:port> using <node>`（无 `match` 段） |

要点：
- 源地址常带 `(进程名)` 后缀（如 `(codex)`、`(Google Chrome Helper)`）——inhomo 不消费，解析时丢弃。
- `目的host:port` 的 host 可能是域名或 IP（含 IPv6 `[::1]:80`），按**最后一个 `:`** 切端口。
- `UDP` 亦有对应 `[UDP] …` 行；明文 HTTP 只关心 TCP。

## 4. 核心：一行 = 一条 TCP 连接**建立**，不是一个包、也不是一个请求

这是理解全部语义的关键。`tunnel.go` 的 `handleTCPConn` 里，`logMetadata` **每条连接只调用一次**，
且在**代理拨号成功之后、数据转发之前**：

```go
remoteConn, err := retry(ctx, func(...) { remoteConn, err = proxy.DialContext(...) }, ...)
if err != nil { return }
logMetadata(metadata, rule, remoteConn)   // ← 连接建立时打这一行
remoteConn = statistic.NewTCPTracker(...)
...
handleSocket(conn, remoteConn)            // ← 之后收发数据全程不再打日志
```

因此：

- **一条 TCP 连接 → 一行日志**（在建立那一刻）。连接建立后真正流过的数据包**一个都不打**。
- `handleTCPConn` 每接受一条入站 TCP 连接调用一次。

**例子**：文首那 5 行都是 `chatgpt.com:443`，但源端口 `55086→55090` **各不相同 = 5 条独立的 TCP 连接**
（codex 的连接池/并发请求），每条建立时各打一行。不是同一条连接发了 5 个包。

## 5. 出境节点 / 链路格式

`using` 之后是出境节点，真机上常是 **`分组名[真实节点|倍率]`** 结构：

- `🚀 节点选择[🇺🇸美国HY2-06|1.0X]` → 分组「🚀 节点选择」选中的末端节点是 `🇺🇸美国HY2-06|1.0X`。
- `🎯 全球直连[DIRECT]` → 末端是 `DIRECT`（其实是直连）。

判定 DIRECT/REJECT、解析地区，**必须取方括号里的末端节点**，不能只看整串——否则
`全球直连[DIRECT]` 会被误当成"经代理"（inhomo 早期真机 bug，见 ADR / 工单 T06）。
`|1.0X` 之类是订阅给节点名带的倍率标注，属节点名的一部分，inhomo 原样保留（不截断，
以免误伤 `🇭🇰香港|IEPL|01` 这种名字自带 `|` 的节点）。

## 6. 拨号失败 = warning 行，格式不同

拨号失败走的是 `logMetadataErr`，`type` 为 `warning`，且**没有 ` using ` 段**：

```
[TCP] dial 🍎 苹果服务 (match DomainSuffix/apple.com) 198.18.0.1:64225 --> 1-courier.sandbox.push.apple.com:80 error: dial tcp ...: connect: connection refused
```

inhomo 的解析器因其缺 ` using ` 而安全跳过——所以 `audit` 统计的是**真正建立、明文确实流出去了**的连接。

## 7. 对 inhomo 的含义

「一行 = 一条连接」这个前提，决定了两个子命令的取舍：

| | keep-alive（一条连接跑多请求） | 连接池（对同一 host 开多条连接） |
|---|---|---|
| 事件计数 | 只算 **1 条**（对请求数**低估**） | 算 **N 条** |
| `logs`（原样） | 显示该连接的 1 行 | 显示 N 行 |
| `audit`（去重） | 1 行 | 按 `(出境节点, 目的host)` **收敛成 1 行**（带"又 ×N"） |

- 想看**每一条连接**（含并发、含 443）→ `inhomo logs`。
- 想看**"哪些 host 在明文泄露"的态势**（去噪去重）→ `inhomo audit`。

## 8. 日志存在哪、怎么保留、怎么关

分两层，别混淆：**内核不落盘，GUI 才落盘。**

### 8.1 内核层：不落盘

mihomo 内核**不写任何日志文件**（源码核实 `log/log.go`：只 `SetOutput(os.Stdout)`，无文件句柄/轮转/log-file 配置）。它只往 **stdout**（logrus）+ **内存观察管道**（给 `/logs` API）吐。纯内核角度日志是**内存态**，不落盘。

### 8.2 GUI 层：捕获 stdout 落盘（以 Clash Verge Rev 为例）

GUI 把内核进程的 **stdout 捕获**下来写成文件（其它客户端路径/策略不同）。Clash Verge Rev 实测：

- **位置**：`~/Library/Application Support/io.github.clash-verge-rev.clash-verge-rev/logs/`
  —— 是**应用数据目录，不是 `/tmp`** → **macOS 不会清、重启也在**。
  （`/tmp/verge/` 里只有 socket + pid/lock 等运行时文件，**没有日志**；那些 macOS 重启可能清，Verge 启动会重建。）

| 文件 | 内容 | 含 `[TCP]` 访问记录？ |
|------|------|----------------------|
| `logs/service/service_*.log` | 内核 stdout（当前运行） | **有**（目的 host + 出境节点，逐连接一行） |
| `logs/sidecar/sidecar_*.log` | 旧内核 stdout | 有（历史） |
| `logs/latest.log`、`logs/<日期>.log` | Verge **自身 App 日志** | 无（只有 `[Config]`/`[System]`） |

> ⚠️ **隐私**：在 `log-level: info` 下，**你每条连接的目的域名 + 节点都被写进 `service` 日志**。速率不低（实测 ≈60 行/分钟），`service` 日志按 **128K 滚动**成多个文件；旧文件会被自动清理，所以访问记录在盘上**只留近期（约当天/当次会话），并非数月**——能跨两个月的是不含 `[TCP]` 的小体积 App 日志（`latest.log`/`<日期>.log`）。整个 `logs/` 目录通常只有几 MB（其中可能夹杂个别未被清理的陈旧大文件）。

### 8.3 保留策略：GUI 自维护（不是 macOS）

Clash Verge Rev 在 `verge.yaml` 里控制：

- `auto_log_clean`：自动清理档位（对应 UI「自动清理日志」下拉；实测保留窗口可达数月）。
- `app_log_max_size` / `app_log_max_count`：App 日志的大小 / 数量上限。
- 文件命名：`latest.log` 当前，历史按 `<日期>.log` 归档。

### 8.4 控制开关（都能在 Clash Verge 里配）

1. **内核日志等级**（决定 `service` 日志里有多少访问记录）：设置 → Clash 内核 → **日志等级**，`silent/error/warning/info/debug`。设 `silent` → 内核不吐 `[TCP]` → **访问记录不再落盘**。对应配置键 `log-level`。
2. **App 日志等级**：设置 → Verge 设置 → **App 日志等级**（`app_log_level`）。
3. **自动清理**：设置 → **自动清理日志**（`auto_log_clean`）。

### 8.5 与 inhomo 的关系

- 把内核 Log Level 调成 `silent`/`warning` **不影响 inhomo**——因为 `log-level` 只 gate **stdout**（GUI 落盘的那份），**不 gate `/logs` API 的订阅投递**（见 §2）。所以可以「**关掉内核日志落盘保护隐私 + inhomo 照常经 `/logs` 实时审计**」两不误。
- inhomo 自身**默认不落盘**，只有 `--out` 才写 JSONL，且只写「明文 HTTP 泄露」这一小撮，不是全量访问记录。

---

术语定义见 `../CONTEXT.md`（「明文 HTTP 连接」「出境节点」「明文 HTTP 泄露事件」）。
