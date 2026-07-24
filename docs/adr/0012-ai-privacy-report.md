# AI 隐私周报：只发聚合、查运行中的 serve、provider 抽象

office-hours 设计《AI 隐私分析师之「域名归类」》Approach B 第三层：把连接态势用**自然语言**讲清楚（`inhomo report`）——这正是离线清单做不了、LLM 真正擅长的「综述」。难点是隐私（inhomo 本身是隐私工具，不能为了写报告反把你访问的域名喂给云端）与并发（DuckDB 单写锁）。

## 决策

- **只发聚合，绝不发原始域名**：report 只取**聚合**素材——追踪器占比 + 归属公司（公司名，非域名）、出境节点 top、节点地区 top。这些都不含「你访问了哪些站」。喂给 LLM 的提示词由 `buildReportPrompt` 从这些聚合拼成，`reportData` 结构本身就不含 host 字段——隐私是**构造保证**，非事后过滤。
- **查运行中的 serve、不直接开库**：`inhomo report` 从运行中的 `serve` 的 `/api`（`/api/trackers`、`/api/aggregate?by=node|region`）取聚合，而非直接开 DuckDB。原因：DuckDB **单进程持锁**，守护 serve 正在写库时 report 直接开库会 `Conflicting lock`。走 HTTP 既避锁，又复用 T42 的接口。代价：report 需要 serve 在跑（`--addr` 指定，默认 `127.0.0.1:8566`）。
- **provider 抽象成接口**：`ai.Provider{ Generate(ctx, prompt) }`，v0 实现 Anthropic Messages API（`x-api-key` + `anthropic-version` 头）。抽象便于注入 stub 测试、也为 T43 复用与将来换/加提供方留位。`--ai-base-url` 可指向兼容代理或测试 stub。
- **key 走 env / 配置，不进命令行历史**：API key 由 `INHOMO_AI_API_KEY` 环境变量或 `~/.inhomo/config.yaml` 的 `ai-api-key` 提供（沿用 viper flag>env>config 优先级）；虽注册了同名 flag 供优先级链，但**建议勿在命令行明文传**。未配置 → 明确报错、不静默。

## 取舍

- **HTTP 查 serve vs 直接开库**：直接开库更独立但撞单写锁；查 serve 避锁、复用接口，代价是耦合一个在跑的 serve。对「总结我常驻实例所见」的报告，这耦合自然。
- **只发聚合 vs 发更细数据**：只发聚合牺牲了报告的细粒度（不能点名具体域名），但守住隐私红线——对隐私工具是正确取舍。真要点名，也应由本地数据在提示词外渲染，而非发给云端。
- **默认模型 `claude-sonnet-5`**：综述任务够用且质量稳；`--ai-model` 可换（如更省的 Haiku）。

## v1 不做

流式输出；报告落库/历史对比；把 T43 的 AI 残差类别并进报告（T43 完成后自然增强）；多 provider 并存；report 在无 serve 时直接开只读库（DuckDB 跨进程只读仍受锁限制，留作后续）。
