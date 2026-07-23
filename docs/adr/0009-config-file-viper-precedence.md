# 引入配置文件（Viper：config + env + flag 优先级）

inhomo 从「自用脚本」走向可分发工具（「打包分发」epic）。其中要用 `brew services` 让它**后台常驻**跑 `serve`。但常驻服务以默认参数启动，而用户的 mihomo 常走 unix-socket（如 `unix:///tmp/verge/verge-mihomo.sock`），默认 `--controller 127.0.0.1:9090` 连不上；让服务拿到用户的 controller/secret，靠编辑 launchd/systemd 定义体验很差。ADR-0003（采用 cobra）当初把 `--controller`/`--secret` 定为 root 持久 flag、**只能命令行传**。

## 决策

- **引入配置文件 `~/.inhomo/config.yaml` + `INHOMO_*` 环境变量**，用 `spf13/viper` 统一解析，覆盖连接/运行参数：`controller`/`secret`/`db`/`traffic-interval`/`addr`/`http-ports`/`window`。
- **优先级：显式 flag > 环境变量 > 配置文件 > 内置默认。** 直接吃 viper `BindPFlag` 的天然语义——被显式改过的 flag（`flag.Changed`）最高、未改过的 flag 默认最低。
- **统一入口**：root 的 `PersistentPreRunE` 为被调用的子命令建好 viper（读文件 + 绑 env + 绑该命令全部 flag），挂到 `cmd.Context`；各读取点改走 `cfgOf(cmd)`，不再直接 `cmd.Flags().Get*`。`serve`/`record`/`audit`/`logs` 共享同一份。
- **文件处理**：缺文件 → 回落默认、不报错；存在但解析失败 → 报错，不静默吞。

## 取舍

- **Viper vs 轻量手写**：选 **Viper**——一步拿到 config + env + flag 绑定与多格式，省样板。代价是较重的传递依赖（`afero`/`cast`/`gotenv` 等）。这是继 ADR-0003 之后**仅限 CLI 层**的又一次引依赖；核心 `detect`/`logstream`/`sink`/`aggregate`/`store` 仍不受影响。
- **键面**：上述 7 个是**文档化**的配置面。因用 `BindPFlags` 统一绑定，`level`/`out` 也顺带经同一优先级解析——这是统一机制的无害超集，非单独特性。
- **固定 `~/.inhomo/config.yaml`，不加 `--config`**：v0 够用（与库文件同目录）；测试用 `$HOME` 覆盖即可验全链路，无需自定义路径。

## 重访 ADR-0003

`--controller`/`--secret` 不再是 flags-only——现在也可由 config 文件 / 环境变量提供；但**显式 flag 仍最高优先**，ADR-0003「持久 flag 跨子命令共享」的定位不变，只是多了两条更低优先的来源。

## 被 ADR-0010 重访

「内置默认」这一最底层，对 `controller` 而言不再是静态 `127.0.0.1:9090`：ADR-0010 让它先[自动发现本机 mihomo](0010-controller-autodiscovery.md)、发现不到才回退 9090。更高优先的三层（显式 flag > env > config）语义不变——任一层显式给了 `controller` 即完全不发现。

## v1 不做

`--config` 自定义路径；配置热重载；按子命令分节的配置；把 secret 从明文配置挪到钥匙串。
