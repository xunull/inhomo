# 引入 cobra 作为 CLI 框架（放弃零第三方依赖）

项目要从单命令拆成多子命令（`audit` 明文 HTTP 泄露审计 + `logs` 原样看 mihomo 日志，后续可能更多）。标准库 `flag` 也能做子命令（`NewFlagSet` + 手写分发），且能保持此前的零第三方依赖。但我们选 **cobra**：子命令路由、持久 flag（`--controller`/`--secret` 跨子命令共享）、自动 help 的体验明显更好，且预期子命令会继续增加。

代价：**放弃零依赖**——引入 `spf13/cobra` 及其传递依赖（`spf13/pflag` 等）。这是自 ADR-0002「消费端零第三方依赖」以来的一次有意反转，**仅限 CLI 层**；核心的 `detect` / `logstream` / `sink` / `aggregate` 仍保持标准库、无外部依赖。

CLI 由此变为 `inhomo <子命令> -flags`（原来的 `inhomo -flags` 不再可用，属早期项目可接受的破坏性变更）。

## 后续修订

**ADR-0009** 修订了本 ADR：`--controller`/`--secret` 不再只能命令行传，现在也可由 `~/.inhomo/config.yaml` 或 `INHOMO_*` 环境变量提供（显式 flag 仍最高优先）。持久 flag 跨子命令共享的定位不变。
