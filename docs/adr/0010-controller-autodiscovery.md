# 本机 mihomo 自动发现（零参数连上，回退 9090）

inhomo 走向可分发工具后，最常见的本机 mihomo 是 **Clash Verge Rev**：它只把 external-controller 开在 unix socket（`unix:///tmp/verge/verge-mihomo.sock`）+ 一个非默认 TCP 端口（如 `127.0.0.1:9097`）上、且带非空 secret，三者只存在于 Verge 生成的运行时配置里。于是用户每次都得手敲 `--controller unix://… --secret …` 才连得上——[后台常驻](0009-config-file-viper-precedence.md)时更别扭。ADR-0009 引入配置文件缓解了「反复传参」，但用户仍要先自己找出那串 socket 路径与 secret 填进去。本 ADR 让**零参数**就能连上本机 mihomo。

## 决策

- **零参数自动发现**：当 `controller` **无任何显式来源**（`--controller` 未改 + 无 `INHOMO_CONTROLLER` env + `~/.inhomo/config.yaml` 无 `controller` 键）时，`serve`/`record`/`audit`/`logs` 自动发现本机 mihomo 并连上。四命令共用同一 `newClient` 接缝，一处接线全覆盖。
- **机制 = 读配置 + 探活**：读本机已知客户端生成的运行时 mihomo 配置 → 解析出候选 `(controller, secret, 来源)`（`external-controller` 走 TCP、`external-controller-unix` 走 socket，同一配置内 unix 优先）→ 逐个用 `/version` 探活（带上 secret、短超时）→ 用**第一个活的**（200 = 端点活着且 secret 被接受）。
- **支持的客户端（固定来源顺序）**：① **Clash Verge Rev**（GUI 主场景，按 OS 定位其 app-data 下 `config.yaml`）；② **裸 mihomo**（`~/.config/mihomo/config.yaml`，覆盖非默认端口 / 带 secret 的裸实例）。多个来源都活时按此顺序取第一个。新增客户端只是往这张来源表再加一条，解析器/探活/优先级全复用。
- **只填空、显式恒赢、all-or-nothing**：`controller` 一旦显式给出就**完全不发现**、原样尊重；`secret` 只在未显式时用发现值，已显式则保留显式值（只借发现到的 controller）。
- **发现不到就回退**：无配置 / 都不活 → 回退内置默认 `127.0.0.1:9090` 照常连（向后兼容：原本 9090 上的裸 mihomo 零参数仍能用）。守护进程场景**不阻塞、不交互**。
- **透明不泄密**：启动行打印发现到的 controller + 来源客户端（或回退提示）；**secret 绝不打印/记录**。

## 取舍

- **读配置 vs 裸探端口**：裸探 `127.0.0.1:9090` 拿不到 socket 路径、更拿不到 secret，故必须读客户端配置。代价是要知道各客户端把配置写在哪、键名是什么——用**固定的来源顺序 + 已知路径**表来收敛，新增客户端只是往表里加一条（T40 加裸 mihomo 源即如此）。
- **探活成本**：本机/socket 探活近乎瞬时，超时只在「有配置但 mihomo 没跑」时才吃满（≤ 候选数 × 1s）；无配置时零探测、瞬间回退。对非交互守护进程可接受。
- **`/version` 200 的语义**：即便 mihomo 的 `/version` 不强制鉴权，我们连 controller 与 secret 是**成对**从同一份配置读出的，200 已足够证明「这个 controller 现在能用、且我们带的 secret 是对的那把」。
- **TDD 接缝**：纯函数「解析 mihomo 配置 → 候选列表」以 fixture 单测；网络探活作为可注入的 `probeFunc` 不纯依赖，两侧都可测。

## 重访 ADR-0009

ADR-0009 定的优先级「显式 flag > env > config > **内置默认**」不变；本 ADR 只改写最底层**「内置默认」**对 `controller` 的含义：由静态 `127.0.0.1:9090` 变为「**先自动发现本机 mihomo，发现不到才回退 `127.0.0.1:9090`**」。更高优先的三层（显式 flag / env / config）语义完全不动——任一层显式给了 `controller`，就走 all-or-nothing、完全不发现。

## v1 不做

Windows 上的客户端路径；mihomo 装在自定义 `-d` 目录时的发现；其它 GUI 客户端；发现多个活控制器时的交互式选择（固定按来源顺序取第一个）；把发现结果缓存到磁盘。
