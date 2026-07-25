# inhomo

**English** · [简体中文](./README.zh-CN.md)

**Audit plaintext-HTTP leaks through mihomo egress proxy nodes, record every connection event into an embedded DuckDB, and analyze it in a built-in React dashboard.**

inhomo subscribes to the `/logs` stream of [mihomo](https://github.com/MetaCubeX/mihomo) (the Clash Meta core), parses each connection log line, and does two things:

1. **Audit plaintext-HTTP leaks** — surface traffic that goes out as plaintext HTTP (destination port `80` by default) *and* is relayed through a **foreign / untrusted proxy node**. Such content is readable by the relaying node — a real privacy-exposure surface.
2. **Full connection analytics** — write every connection event (not just leaks) into an embedded DuckDB, and analyze it via an in-process web server + React dashboard (top domains, per-app profile, egress-node share, region distribution, time series, …).

The whole program is a **single binary**: CLI, web server, DuckDB, and the frontend dashboard are all bundled into one executable that runs locally — your data stays on the machine.

---

## Table of contents

- [What it solves](#what-it-solves)
- [How it works](#how-it-works)
- [Requirements](#requirements)
- [Install via Homebrew](#install-via-homebrew)
- [Build from source](#build-from-source)
- [Quick start](#quick-start)
- [Command reference](#command-reference)
- [Local mihomo auto-discovery](#local-mihomo-auto-discovery)
- [Configuration](#configuration)
- [Run as a background service](#run-as-a-background-service)
- [Web analytics API](#web-analytics-api)
- [Data model](#data-model)
- [Connecting to mihomo](#connecting-to-mihomo)
- [Core concepts](#core-concepts)
- [Design decisions](#design-decisions)
- [Project layout](#project-layout)
- [Privacy and concurrency](#privacy-and-concurrency)
- [Development](#development)

---

## What it solves

In TUN mode, all local traffic exits through mihomo. Some of it is **plaintext HTTP**: the URL, Host, even the body carry no TLS protection. If such a connection is also routed through a **foreign proxy node**, its content is readable by that node's operator — a real, and normally invisible, privacy-leak path.

inhomo makes that path explicit. The alert condition is neither "any HTTP" nor "any proxied connection," but their intersection — **a plaintext-HTTP connection that went through an egress node** (`chains` is not `DIRECT`/`REJECT`). Along the way it archives every connection so you can review "which apps visited which domains, through which nodes, to which regions."

## How it works

mihomo's `/logs` is a **plain HTTP GET stream** (newline-delimited JSON, no WebSocket needed). Each `[TCP]` log line is printed by mihomo **once, at the moment the connection is established**, so inhomo's smallest unit of observation is "one TCP connection" — not an individual HTTP request, not a packet.

```
mihomo  ──GET /logs?level=info (newline-delimited JSON stream)──►  inhomo
                                                    │
                                        detect.Parse (parse each [TCP] connection log)
                                                    │
                        ┌───────────────────────────┼───────────────────────────┐
                        ▼                            ▼                            ▼
                  audit: plaintext HTTP      record: all connection       logs: print mihomo
                  + egress node → alert       events → embedded DuckDB     log lines verbatim
                  (terminal / JSONL)                 │
                                                    ▼
                                          serve: in-process web server
                                          ├─ React dashboard (/)
                                          └─ analytics API (/api/*)
```

Two auxiliary commands center on **tracker identification**: `tracker` fetches a local tracker database that lets `audit` and the dashboard label destination domains as "known tracker + owning company"; `report` queries a running `serve`'s `/api` and uses an LLM to write a natural-language privacy digest (aggregates only, never raw domains).

A connection log line looks like:

```
[TCP] 192.168.1.2:54321(curl) --> example.com:80 [match RuleSet | GEOIP] using 🚀 节点选择[🇺🇸美国HY2-06|1.0X]
```

From it inhomo parses: the process (`curl`), destination host/port (`example.com:80`), the matched rule, the egress node (the last real node in `chains`), and the region inferred from the node name (`🇺🇸` → `US`).

## Requirements

- **Go 1.26+**
- **A C compiler + CGO** — DuckDB is embedded via [go-duckdb](https://github.com/marcboeker/go-duckdb) using CGO, so building `record`/`serve` needs `CGO_ENABLED=1` (macOS ships clang; Linux needs gcc/clang).
- **Node.js** (only if you want to **rebuild the frontend**) — the built frontend `web/dist` is committed, so a bare `go build` embeds it without Node.
- **A running mihomo** with `external-controller` enabled (TCP port or Unix socket).

## Install via Homebrew

Prebuilt binaries (macOS / Linux, each arm64 + amd64) are distributed through a Homebrew tap — no Go toolchain required:

```bash
brew tap xunull/tap        # tap repo: github.com/xunull/homebrew-tap
brew trust xunull/tap      # Homebrew 6.x+: trust the third-party tap, or install is refused
brew install inhomo        # or in one shot: brew install xunull/tap/inhomo
```

> **`brew trust` is a required step**: since Homebrew 6.x, third-party taps sit behind a trust gate; running `brew install` without `brew trust` fails with `Refusing to load formula ... from untrusted tap`. This is Homebrew's own security mechanism, not something this project can waive. After `brew trust xunull/tap` (or `brew trust --formula xunull/tap/inhomo` to trust just this one formula) install works normally.

Upgrade with `brew upgrade inhomo`, remove with `brew uninstall inhomo`. You still need a running mihomo (see below).

> **On "unsigned"**: the binaries are not Apple-notarized, but **installing via Homebrew needs no manual approval** — Homebrew downloads with its own curl and does not stamp the `com.apple.quarantine` attribute (that comes from browser downloads), so it runs right after install. Only if you bypass brew and download the Release `tar.gz` in a browser do you need `xattr -d com.apple.quarantine ./inhomo` to clear it.

## Build from source

Needs Go + CGO (see [Requirements](#requirements) above); use this path when hacking on the code or when Homebrew isn't available.

```bash
# After cloning, one command builds the frontend + embeds it + go build, producing the single binary ./inhomo
make

# To change the frontend, install deps first, then build it separately
make deps        # npm install (first time, or after package.json changes)
make frontend    # build web/dist (needed by go:embed)

# Build Go only (uses the committed web/dist, no Node needed)
make build       # equivalent to CGO_ENABLED=1 go build -o inhomo .

# Full Go test suite
make test
```

## Quick start

> Using **Clash Verge Rev**? You can omit `--controller` / `--secret` — inhomo will [auto-discover your local mihomo](#local-mihomo-auto-discovery) and connect. The commands below spell the arguments out for generality.

Three steps to the dashboard (assuming your mihomo controller is at the default `127.0.0.1:9090`):

```bash
# 1) build
make

# 2) record connections and serve the web UI at once (default DB ~/.inhomo/connections.duckdb)
./inhomo serve --controller 127.0.0.1:9090 --secret <your-secret>

# 3) open in a browser
open http://127.0.0.1:8566/
```

Generate some traffic (just browse normally) and the dashboard fills up with KPIs, top domains/nodes, and the time-series chart.

Just want to check connectivity and see what mihomo is logging:

```bash
./inhomo logs --controller 127.0.0.1:9090 --secret <your-secret>
```

Just want to audit plaintext-HTTP leaks (no DB, leaks printed to the terminal):

```bash
./inhomo audit --controller 127.0.0.1:9090 --secret <your-secret>
```

## Command reference

All commands share two root persistent flags:

| Flag | Default | Description |
|---|---|---|
| `--controller` | `127.0.0.1:9090` | mihomo external-controller. TCP like `127.0.0.1:9090`, or a Unix socket like `unix:///tmp/verge/verge-mihomo.sock`. **If omitted, it [auto-discovers your local mihomo](#local-mihomo-auto-discovery)** and only falls back to `127.0.0.1:9090` when nothing is found. |
| `--secret` | `""` | The external-controller secret (leave empty if auth is off). |

### `inhomo audit`

Identify and record "plaintext HTTP + through an egress node" leak events. The terminal de-duplicates by `(egress node, destination host)` within a time window so it doesn't flood; optionally append every raw event to a JSONL file.

| Flag | Default | Description |
|---|---|---|
| `--level` | `info` | Log level to subscribe to; connection logs need `info`. |
| `--http-ports` | `80` | Destination ports treated as plaintext HTTP (comma-separated, e.g. `80,8080`). |
| `--out` | `""` | JSONL output file path (empty = terminal only, no file). |
| `--window` | `5m` | Terminal aggregation window: the same `(node, host)` surfaces only once per window. |

Example terminal output:

```
15:04:05  明文HTTP泄露  example.com:80  →  🇺🇸美国HY2-06|1.0X [US]  规则:GEOIP  (过去 5m0s 内又 ×3)
```

After running [`inhomo tracker update`](#inhomo-tracker) once, leak lines that hit a known tracker are additionally annotated with the owning company, e.g. `… 规则:GEOIP  [已知追踪器 · Google]`. Without the data it prints a one-time hint and audits as usual (no error).

### `inhomo logs`

View mihomo logs verbatim (line-by-line payload + a level tag), for checking connectivity or watching what the core is emitting.

| Flag | Default | Description |
|---|---|---|
| `--level` | `info` | Log level to subscribe to: `info` / `warning` / `error` / `debug`. |

### `inhomo record`

Write every connection event (**all of them, not just leaks**) into the embedded DuckDB for later analysis.

| Flag | Default | Description |
|---|---|---|
| `--level` | `info` | Log level to subscribe to; connection logs need `info`. |
| `--db` | `~/.inhomo/connections.duckdb` | DuckDB file path. Empty = default path; a `~/` prefix expands to home; missing directories are created. |

### `inhomo serve`

A superset of `record`: record connections and serve the web UI in the same process, hosting the React dashboard and the analytics API.

| Flag | Default | Description |
|---|---|---|
| `--level` | `info` | Same as `record`. |
| `--db` | `~/.inhomo/connections.duckdb` | Same as `record`. |
| `--addr` | `127.0.0.1:8566` | Web listen address (loopback-only and unauthenticated by default; a non-loopback address prints a warning). |

> Recording runs in the background; if the recording side stops (e.g. the `/logs` connection fails) the web server is shut down with it, so you never have "recording dead, web idling."

### `inhomo tracker`

Manage the tracker-identification data (DuckDuckGo Tracker Radar's "domain → owning company" table). The data is CC BY-NC-SA 4.0 and is **not** shipped inside the binary; this command **fetches it at runtime** to `~/.inhomo/tracker-radar.json`, and matching is done **offline** afterward (see ADR-0011).

```bash
inhomo tracker update    # fetch/update the domain table (~10MB, ~38k known tracker domains)
```

Once fetched, `audit`'s leak lines are annotated with the matched known tracker and its owner. When the data isn't fetched or the network is down, the classifier is empty, leak lines carry no annotation, and nothing errors (graceful degradation). v0 does "known tracker + owner" only; fine-grained categories (advertising/analytics/…) are left for later.

> What the downloaded file is, its format, and how it's used: see [docs/tracker-radar-data.md](./docs/tracker-radar-data.md) (written in Chinese).

### `inhomo report`

Use an LLM to summarize the connection picture into a natural-language privacy digest. **Only aggregates** (tracker share + owning companies, top egress nodes / regions) are sent to the model — **never raw visited domains** (see ADR-0012). It pulls the aggregates from a **running `serve`**'s `/api` (to avoid the DuckDB single-writer lock), so a `serve` must be running.

```bash
export INHOMO_AI_API_KEY=sk-...            # or put ai-api-key in ~/.inhomo/config.yaml (avoid passing it on the command line)
inhomo report --since 7d                   # defaults: Anthropic, model claude-sonnet-5, querying the serve at 127.0.0.1:8566

# Use DeepSeek or another OpenAI-compatible platform: pick the openai provider + its base URL/model (DeepSeek is NOT Anthropic-compatible)
inhomo report --ai-provider openai --ai-base-url https://api.deepseek.com --ai-model deepseek-chat --since 7d
```

| Flag | Default | Description |
|---|---|---|
| `--addr` | `127.0.0.1:8566` | Address of the running serve to query (aggregates come from its `/api`). |
| `--since` | `7d` | Time window (e.g. `7d` / `24h`). |
| `--out` | `""` | Write the report to this file (empty = print to terminal). |
| `--ai-provider` | `anthropic` | `anthropic` or `openai` (`openai` covers DeepSeek / OpenAI / Groq / local Ollama, …). |
| `--ai-model` | `claude-sonnet-5` | Model to generate with (change it when you change providers, e.g. `deepseek-chat` for DeepSeek). |
| `--ai-api-key` | `""` | API key (prefer the `INHOMO_AI_API_KEY` env var or the config file). |
| `--ai-base-url` | `""` | API base URL (defaults to each provider's official one; DeepSeek uses `https://api.deepseek.com`). |

## Local mihomo auto-discovery

**Zero-argument by default**: with no `--controller` (and no `INHOMO_CONTROLLER` env var, and no `controller` key in `~/.inhomo/config.yaml`), `serve` / `record` / `audit` / `logs` auto-discover your local mihomo and connect — no need to type `--controller unix://… --secret …`.

- **How it finds it**: it reads known clients' runtime mihomo config in a fixed priority order, extracts `external-controller` (TCP), `external-controller-unix` (socket) and `secret`, probes each with `/version`, and uses the **first live one** (unix socket first within a config). Supported clients (source order):
    1. **[Clash Verge Rev](https://github.com/clash-verge-rev/clash-verge-rev)** (GUI, the main case): macOS at `~/Library/Application Support/io.github.clash-verge-rev.clash-verge-rev/config.yaml`, Linux at the corresponding app-data directory.
    2. **Bare mihomo** (`~/.config/mihomo/config.yaml`): covers a bare mihomo on a non-default port or with a secret set (the plain `127.0.0.1:9090`-with-no-secret case is already covered by the fallback below, no config read needed).
- **Fallback when nothing is found**: no Verge, or it isn't running → fall back to `127.0.0.1:9090` and connect as usual (a bare mihomo on 9090 still works zero-arg). The startup line clearly states whether it "auto-discovered" or "fell back."
- **Explicit always wins (all-or-nothing)**: the moment you give `--controller` explicitly (or set `controller` via env / config file), discovery is **skipped entirely** and your value is used as-is.
- **No secret leakage**: the startup line only prints the discovered controller and its source client; the **secret is never printed or logged**.

```bash
# With Verge, this is all you need — it finds the socket and carries the secret:
./inhomo logs
# [inhomo] 从 Clash Verge Rev 自动发现 controller unix:///tmp/verge/verge-mihomo.sock
```

The decision and priority details are in ADR-0010 (which also revisits the "built-in default" layer of ADR-0009).

> Only the known clients above are read. Other sources (mihomo installed in a custom `-d` directory, or a different GUI) are not auto-discovered yet — pass `--controller`/`--secret` explicitly, or put them in `~/.inhomo/config.yaml`.

## Configuration

Besides command-line flags, the parameters above can also live in **`~/.inhomo/config.yaml`** or come from **`INHOMO_*` environment variables**. Precedence of the three:

> **explicit flag > environment variable > config file > built-in default**

For `controller`, that "built-in default" layer is no longer a hard-coded `127.0.0.1:9090` — it [auto-discovers your local mihomo](#local-mihomo-auto-discovery) and only falls back to `127.0.0.1:9090` when nothing is found (see ADR-0010).

This is especially handy when [running as a background service](#run-as-a-background-service) (`brew services`): put the controller in the config and the service needs no repeated `--controller unix://…` on the command line. A missing file falls back to defaults (no error); a present-but-malformed file errors out. See ADR-0009.

`~/.inhomo/config.yaml` example (keys match the flag names; one file is shared by `serve`/`record`/`audit`/`logs`, each taking what it needs):

```yaml
controller: unix:///tmp/verge/verge-mihomo.sock
secret: ""
db: ~/.inhomo/connections.duckdb
traffic-interval: 3s
addr: 127.0.0.1:8566
http-ports: "80"
window: 5m
```

Environment variables = `INHOMO_` prefix + the uppercased key with hyphens turned into underscores:

```bash
export INHOMO_CONTROLLER=unix:///tmp/verge/verge-mihomo.sock
export INHOMO_TRAFFIC_INTERVAL=0   # disable traffic sampling
```

## Run as a background service

An inhomo installed via Homebrew ships a service definition; `brew services` runs it in the background with login/boot autostart (launchd on mac, systemd on linux — one definition covers both):

```bash
brew services start inhomo     # start background serve + login/boot autostart
brew services stop inhomo      # stop
brew services restart inhomo   # restart to pick up config.yaml changes
brew services info inhomo      # status
```

The service runs `inhomo serve` with **no command-line arguments** — controller / secret / db / addr all come from [`~/.inhomo/config.yaml`](#configuration). So write the config first, especially for a unix-socket mihomo:

```yaml
# ~/.inhomo/config.yaml
controller: unix:///tmp/verge/verge-mihomo.sock
secret: ""
```

> **Don't add `sudo`**: `brew services start inhomo` (without sudo) runs as your user, so `$HOME` is your home directory and it can find `~/.inhomo/config.yaml` and the default DB `~/.inhomo/connections.duckdb`. With sudo it runs as root and can't read your config.
>
> **`INHOMO_*` env vars don't apply to the daemon**: launchd/systemd don't load your shell (they don't read `~/.zshrc`), so for the service always use config.yaml, not env.
>
> **Don't fight over the DB with a manual `serve`**: the service is already writing the default DB, so don't also run `inhomo serve`/`record` on the same DB manually (DuckDB single-writer lock, see [Privacy and concurrency](#privacy-and-concurrency)).
>
> Service logs go to `$(brew --prefix)/var/log/inhomo.log`; the dashboard is at `http://127.0.0.1:8566/` as usual.

## Web analytics API

`serve` exposes the following endpoints (same origin — it also hosts the frontend). Unknown `/api/*` return a 404 JSON; other unmatched routes fall back to the dashboard `index.html` (SPA).

| Method | Path | Description |
|---|---|---|
| GET | `/` | React dashboard |
| GET | `/api/summary` | KPI overview |
| GET | `/api/aggregate?by=&since=&limit=` | top-N by dimension |
| GET | `/api/timeseries?since=&bucket=` | connection count per time bucket |
| GET | `/api/trackers?since=&limit=` | tracker exposure: how many connections hit known trackers + top owners |

**Time parameter format**: `since` / `bucket` accept Go durations (`24h`, `90m`) or `7d` (days); an empty `since` means "all time."

**`by` dimension allow-list**: `host` / `process` / `node` / `region` / `port` (other values return 400).

Examples:

```bash
curl 'http://127.0.0.1:8566/api/summary'
# {"total":41,"hosts":22,"processes":7,"nodes":2,"direct":4,"proxied":37,
#  "http":11,"https":30,"earliest":"2026-07-18T14:19:44Z","latest":"2026-07-18T14:19:56Z"}

curl 'http://127.0.0.1:8566/api/aggregate?by=host&since=24h&limit=5'
# [{"key":"api.github.com","count":16},{"key":"chatgpt.com","count":3}, ...]

curl 'http://127.0.0.1:8566/api/trackers?since=7d&limit=5'
# {"total":41,"tracker":12,"owners":[{"owner":"Google","count":8},{"owner":"comScore","count":4}]}
```

`summary` field semantics: `proxied` = through an egress node (`node` non-empty, not `DIRECT`, not `REJECT*`); `direct` = direct; `http`/`https` = connections to destination port 80/443.

## Data model

DuckDB holds a single table `connections`, one row per connection:

| Column | Type | Meaning |
|---|---|---|
| `ts` | TIMESTAMP | When the connection was established |
| `process` | VARCHAR | The process that opened the connection (may be empty) |
| `network` | VARCHAR | Network type (e.g. TCP) |
| `host` | VARCHAR | Destination host |
| `port` | INTEGER | Destination port |
| `rule` | VARCHAR | The mihomo routing rule that matched |
| `node` | VARCHAR | Egress node (last real node in `chains`; `DIRECT` for direct) |
| `region` | VARCHAR | Region inferred from the node name (flag emoji / country words; `unknown` if not inferable) |

"Plaintext-HTTP leak" is just one filtered view of this table; the dashboard panels are its various `GROUP BY`s. The DB file can be opened directly with the `duckdb` CLI for ad-hoc queries.

## Connecting to mihomo

inhomo needs mihomo's `external-controller`. Both forms are supported:

- **TCP** (most clash setups): `--controller 127.0.0.1:9090`
- **Unix socket** (e.g. Clash Verge Rev): `--controller unix:///tmp/verge/verge-mihomo.sock`

If the controller has a `secret`, pass it with `--secret <value>`.

> Clash Verge Rev by default only exposes a Unix socket (`external-controller` is empty); the socket path is usually `/tmp/verge/verge-mihomo.sock`.

## Core concepts

Precise term definitions are in [`CONTEXT.md`](./CONTEXT.md):

- **Plaintext-HTTP connection** — a TCP connection out through mihomo whose destination port is in the HTTP port set (the smallest unit is "connection," not request/packet).
- **Egress node** — the last real proxy node in `chains`; `DIRECT`/`REJECT` don't count.
- **Plaintext-HTTP leak event** — the core alert unit = a plaintext-HTTP connection **and** it went through an egress node.
- **Connection event** — a structured record of one connection parsed from `/logs` (all of them), the basic unit for `record`/analytics.
- **Node region label** — a best-effort country/region parsed from the node name, used only as a classification label, not a hard filter (node IPs aren't available, so no authoritative GeoIP).

## Design decisions

Key trade-offs are recorded in [`docs/adr/`](./docs/adr/) (the ADRs are written in Chinese):

| ADR | Decision |
|---|---|
| [0001](./docs/adr/0001-node-region-by-name-not-geoip.md) | Region from node name, not GeoIP |
| [0002](./docs/adr/0002-logs-stream-as-primary-source.md) | `/logs` stream as the primary source (not the `/connections` snapshot) |
| [0003](./docs/adr/0003-adopt-cobra-cli.md) | Adopt cobra for subcommands |
| [0004](./docs/adr/0004-analytics-app-embed-duckdb.md) | Connection-analytics direction + embedded DuckDB (CGO) |
| [0005](./docs/adr/0005-serve-fiber-web-api.md) | `serve` command + Fiber web analytics API |
| [0006](./docs/adr/0006-embedded-react-dashboard.md) | Embedded React dashboard (Vite + Antd + Recharts, go:embed) |
| [0007](./docs/adr/0007-drilldown-detail-pages.md) | Metric drill-down: filter-slice detail pages + react-router |
| [0008](./docs/adr/0008-traffic-bytes-from-connections.md) | Historical traffic analysis: bytes from `/connections` (a separate traffic dataset) |
| [0009](./docs/adr/0009-config-file-viper-precedence.md) | Config file (Viper: config + env + flag precedence) |
| [0010](./docs/adr/0010-controller-autodiscovery.md) | Local mihomo auto-discovery (zero-arg connect, fall back to 9090) |
| [0011](./docs/adr/0011-tracker-radar-classification.md) | Tracker identification: Tracker Radar fetched at runtime + in-process classification |
| [0012](./docs/adr/0012-ai-privacy-report.md) | AI privacy report: aggregates-only, query a running serve, provider abstraction |

Technical details of mihomo's `/logs` (format, levels, delivery semantics, retention) are in [`docs/mihomo-logs.md`](./docs/mihomo-logs.md).

## Project layout

```
main.go                 entry point (cli.Execute)
internal/
  cli/                  cobra subcommands: audit / logs / record / serve / tracker / report / version
  logstream/            /logs stream client (TCP + Unix socket, auto-reconnect); /version liveness probe
  detect/               connection-log parsing + plaintext-HTTP leak classification + region inference
  aggregate/            audit's (node, host) time-window de-duplication
  sink/                 JSONL output
  store/                embedded DuckDB: writes (Appender) + queries (summary/aggregate/timeseries/trackers)
  tracker/              offline tracker classifier: host → eTLD+1 → known tracker + owner (Tracker Radar, fetched at runtime)
  ai/                   LLM provider abstraction + implementations (Anthropic, OpenAI-compatible like DeepSeek), used by report
web/                    Vite + React + TS dashboard; embed.go packs dist via go:embed
docs/adr/               architecture decision records
CONTEXT.md              domain glossary
Makefile                make = frontend + embed + go build
```

## Privacy and concurrency

- **Loopback-only, no auth by default**: `serve` listens on `127.0.0.1:8566`. Your browsing history is sensitive — don't set `--addr` to a non-loopback address and expose it to the network (it prints a warning).
- **DuckDB single-writer lock**: a `.duckdb` file can be written by only one process at a time. So **don't run `record` and `serve` on the same DB at once** (`serve` already records). If another inhomo process holds the default DB, a second one errors with `Conflicting lock` — use a different `--db` path, or stop the other process.
- **Local by default; outbound traffic is minimal and explicit**: the core path (`audit` / `record` / `serve` / dashboard) stores and queries everything locally with zero outbound traffic. Only two commands touch the network, and both are things you trigger on purpose: `tracker update` **downloads** the tracker database to your machine (it uploads none of your data); `report` sends only **aggregates** (owning companies / nodes / regions + counts, **never raw visited domains**) to your configured LLM. Run neither and nothing goes out.

## Development

```bash
make test                       # full Go test suite (CGO)
go test ./internal/detect/...   # a single package
npm --prefix web run build      # build the frontend alone (tsc + vite)
```

After frontend changes, remember to `make frontend` to rebuild `web/dist` and commit it — `go:embed` bundles the committed build output.
