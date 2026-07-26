package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/spf13/cobra"
	"github.com/xunull/inhomo/internal/store"
	"github.com/xunull/inhomo/internal/tracker"
	"github.com/xunull/inhomo/web"
)

func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "记录连接事件 + 同进程开 Web 分析接口（record 的超集）",
		Args:  cobra.NoArgs,
		RunE:  runServe,
	}
	addRecordFlags(cmd)
	cmd.Flags().String(flagAddr, "127.0.0.1:8566", "Web 服务监听地址（默认仅本机、无鉴权）")
	cmd.Flags().String(flagMuteProcs, defaultMuteProcesses,
		"新增视图里默认折叠的进程（逗号分隔、子串匹配）：目的地由用户自己指定的那类进程")
	return cmd
}

// defaultMuteProcesses 是「用户指定目的地的进程」的内置默认清单（见 CONTEXT 术语、ADR-0014）。
// 只列主流浏览器名、走**子串**匹配：`Google Chrome Helper` 含 `Google Chrome` 故命中；
// 而各类非浏览器的 Helper（小程序容器、AI 客户端、名字里带 Browser 的第三方 App）都不含浏览器名，
// 不会被误伤——它们虽然也由用户点开，但目的地是 App 自己定的，是信号而非噪音。
const defaultMuteProcesses = "Google Chrome,Safari,Firefox,Microsoft Edge"

// splitList 把逗号分隔的配置值切成去空白、去空项的列表（同 --http-ports 的先例）。
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isMutedProcess 判断进程是否属于「用户指定目的地的进程」：大小写不敏感的子串匹配。
// 名单为空 → 一律不折叠（用户显式清空即表示「什么都别替我藏」）。
func isMutedProcess(process string, muted []string) bool {
	p := strings.ToLower(process)
	for _, m := range muted {
		if strings.Contains(p, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// isLoopbackAddr 判断监听地址是否为本机回环（127.0.0.1 / ::1 / localhost）。
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func runServe(cmd *cobra.Command, _ []string) error {
	addr := cfgOf(cmd).GetString(flagAddr)
	if !isLoopbackAddr(addr) {
		fmt.Fprintf(os.Stderr, "[inhomo] ⚠ --addr %s 非本机回环：Web 接口无鉴权，会把你的访问历史暴露给该网络。\n", addr)
	}

	st, err := openStore(cmd)
	if err != nil {
		return err
	}
	defer st.Close() // 退出前落地剩余缓冲

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 记录在后台 goroutine 跑（与 record 同一套逻辑），Web 在主 goroutine。
	// 记录结束（含 /logs 连接失败）→ stop() 取消 ctx，连带关闭 Fiber，避免"记录已死、web 空转"。
	recErr := make(chan error, 1)
	go func() {
		recErr <- recordInto(ctx, cmd, st, "已连接 /logs，边记录边服务 Web…")
		stop()
	}()

	// 追踪器识别数据（`inhomo tracker update` 拉取）：未拉取/损坏 → 空归类器，/api/trackers 全非追踪器、不报错。
	classifier := loadClassifier()

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	registerRoutes(app, st, classifier, splitList(cfgOf(cmd).GetString(flagMuteProcs)))
	if err := registerStatic(app); err != nil {
		return err
	}
	// Ctrl-C（ctx 取消）→ 关闭 Fiber，app.Listen 随即返回。
	go func() {
		<-ctx.Done()
		_ = app.ShutdownWithContext(context.Background())
	}()

	fmt.Fprintf(os.Stderr, "[inhomo] Web 仪表盘：http://%s/ （Ctrl-C 停）\n", addr)
	listenErr := app.Listen(addr)

	stop() // 确保记录 goroutine 收尾（正常关闭时 ctx 已取消，此处幂等）
	recordErr := <-recErr
	if listenErr != nil {
		return listenErr // Listen 出错（如端口占用）优先
	}
	return recordErr
}

// parseDur 解析相对时长（用于 since 与 bucket）：空 → 0；支持 "7d"（天）与 Go 时长（"24h"/"90m" 等）。
func parseDur(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || days < 0 {
			return 0, fmt.Errorf("无效的时长 %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("无效的时长 %q（不接受负值）", s)
	}
	return d, nil
}

// registerStatic 用内嵌的前端 dist 托管仪表盘（SPA：未匹配路由回退 index.html）。
// 必须在 registerRoutes 之后注册，让 /api/* 优先匹配。
func registerStatic(app *fiber.App) error {
	dist, err := web.Dist()
	if err != nil {
		return err
	}
	app.Use("/", filesystem.New(filesystem.Config{
		Root:         http.FS(dist),
		Index:        "index.html",
		NotFoundFile: "index.html",
	}))
	return nil
}

// parseFilter 从 query 解析出一个「过滤切片」：钻取约束（host/process/node/region/port 精确、
// route=direct|proxied 谓词）+ 时间窗 since。非法 port/route/since 返回错误（handler 转 400）。
func parseFilter(c *fiber.Ctx) (store.Filter, error) {
	f := store.Filter{
		Host:    c.Query("host"),
		Process: c.Query("process"),
		Node:    c.Query("node"),
		Region:  c.Query("region"),
	}
	if p := c.Query("port"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return store.Filter{}, fmt.Errorf("无效的 port %q", p)
		}
		f.Port = &n
	}
	switch r := c.Query("route"); r {
	case "", "direct", "proxied":
		f.Route = r
	default:
		return store.Filter{}, fmt.Errorf("无效的 route %q（可选 direct/proxied）", r)
	}
	since, err := parseDur(c.Query("since"))
	if err != nil {
		return store.Filter{}, err
	}
	f.Since = since
	return f, nil
}

// registerRoutes 注册 Web 分析接口。handler 薄：解析过滤切片 + 调 store 查询 + 编码 JSON。
// classifier 供 /api/trackers 与 /api/new 做「host → 归属公司」归类（进程内、无网络）；
// mutedProcs 是「用户指定目的地的进程」清单，仅影响 /api/new 的分组标记（只标记、不删除）。
func registerRoutes(app *fiber.App, st *store.Store, classifier *tracker.Classifier, mutedProcs []string) {
	badReq := func(c *fiber.Ctx, err error) error {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	svrErr := func(c *fiber.Ctx, err error) error {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// /api/summary?<过滤> —— 过滤切片的总量与分布（无参 = 全集，主面板口径不变）。
	app.Get("/api/summary", func(c *fiber.Ctx) error {
		f, err := parseFilter(c)
		if err != nil {
			return badReq(c, err)
		}
		sm, err := st.Summary(f)
		if err != nil {
			return svrErr(c, err)
		}
		return c.JSON(sm)
	})

	// /api/aggregate?by=host&<过滤>&limit=20 —— 过滤切片内按维度 top-N。
	app.Get("/api/aggregate", func(c *fiber.Ctx) error {
		f, err := parseFilter(c)
		if err != nil {
			return badReq(c, err)
		}
		rows, err := st.Aggregate(c.Query("by"), f, c.QueryInt("limit", 0)) // 0 → Aggregate 内兜底默认
		if err != nil {
			if errors.Is(err, store.ErrBadDimension) {
				return badReq(c, err)
			}
			return svrErr(c, err)
		}
		return c.JSON(rows)
	})

	// /api/timeseries?<过滤>&bucket=5m —— 过滤切片内按时间桶的连接数。
	app.Get("/api/timeseries", func(c *fiber.Ctx) error {
		f, err := parseFilter(c)
		if err != nil {
			return badReq(c, err)
		}
		bucket, err := parseDur(c.Query("bucket"))
		if err != nil {
			return badReq(c, err)
		}
		pts, err := st.TimeSeries(f, bucket)
		if err != nil {
			return svrErr(c, err)
		}
		return c.JSON(pts)
	})

	// /api/connections?<过滤>&offset=0&limit=50 —— 过滤切片的原始连接明细（时间倒序，含总数）。
	app.Get("/api/connections", func(c *fiber.Ctx) error {
		f, err := parseFilter(c)
		if err != nil {
			return badReq(c, err)
		}
		pg, err := st.Connections(f, c.QueryInt("offset", 0), c.QueryInt("limit", 0))
		if err != nil {
			return svrErr(c, err)
		}
		return c.JSON(pg)
	})

	// /api/flow?metric=count&<过滤>&since=&limit= —— 两层 App→节点 拓扑（Sankey 数据，每层 top-N + 其它桶）。
	// metric：连接数(count，默认、全量) 或 字节(up/down/total，抽样)——决定边权与取表。
	app.Get("/api/flow", func(c *fiber.Ctx) error {
		f, err := parseFilter(c)
		if err != nil {
			return badReq(c, err)
		}
		g, err := st.Flow(f, c.Query("metric"), c.QueryInt("limit", 0))
		if err != nil {
			if errors.Is(err, store.ErrBadMetric) {
				return badReq(c, err)
			}
			return svrErr(c, err)
		}
		return c.JSON(g)
	})

	// /api/traffic?by=host&metric=total&<过滤>&since=&limit= —— 流量记录上按维度的字节 top-N + 切片总上/下行。
	app.Get("/api/traffic", func(c *fiber.Ctx) error {
		f, err := parseFilter(c)
		if err != nil {
			return badReq(c, err)
		}
		ag, err := st.Traffic(c.Query("by"), c.Query("metric"), f, c.QueryInt("limit", 0))
		if err != nil {
			if errors.Is(err, store.ErrBadDimension) || errors.Is(err, store.ErrBadMetric) {
				return badReq(c, err)
			}
			return svrErr(c, err)
		}
		return c.JSON(ag)
	})

	// /api/exfil?<过滤>&since=&limit=&minUp=&minSampled= —— 按「应用通道」(App,host) 的外发比 top-N，
	// 每行随附采样覆盖率的原始分子分母（见 ADR-0013）。无维度/度量参数：主体与排序都是固定的，
	// 不像 /api/traffic 那样可选 by/metric——外发比不是一种排序方式，是另一种分析。
	app.Get("/api/exfil", func(c *fiber.Ctx) error {
		f, err := parseFilter(c)
		if err != nil {
			return badReq(c, err)
		}
		rows, err := st.Exfil(f, c.QueryInt("limit", 0), int64(c.QueryInt("minUp", 0)), c.QueryInt("minSampled", 0))
		if err != nil {
			return svrErr(c, err)
		}
		return c.JSON(rows)
	})

	// /api/new/count?since= —— 只回一个数字，供主页 KPI 当入口钩子。
	// 与 /new 页面上「未折叠」的部分同口径（都排除 mutedProcs），免得主页说 500、页面只显示 254。
	app.Get("/api/new/count", func(c *fiber.Ctx) error {
		since, err := parseDur(c.Query("since"))
		if err != nil {
			return badReq(c, err)
		}
		n, err := st.CountNewChannels(since, mutedProcs)
		if err != nil {
			return svrErr(c, err)
		}
		return c.JSON(fiber.Map{"count": n})
	})

	// /api/gaps?since= —— 「规则缺口」：兜底连接按可注册域折叠，一行即一条待补的分流规则。
	// 不收过滤切片：这页是「我该先补哪几条规则」的工作台，规则是全局配置、不分切片。
	// since 空 = 全部历史（页面默认传 7d：早已补过规则的域名会一直留在历史里）。
	app.Get("/api/gaps", func(c *fiber.Ctx) error {
		since, err := parseDur(c.Query("since"))
		if err != nil {
			return badReq(c, err)
		}
		fb, err := st.Fallthrough(since)
		if err != nil {
			return svrErr(c, err)
		}
		gaps, ips := groupRuleGaps(fb.Hosts)
		resp := gapsResponse{Gaps: gaps, IPTargets: ips, Bypassed: fb.Bypassed}
		// 累计覆盖的分母含 IP 目标——域名清单累计不到 100%，差额正是 IP 区，如实呈现。
		for _, h := range fb.Hosts {
			resp.TotalConns += h.Conns
			resp.TotalBytes += h.Bytes
		}
		return c.JSON(resp)
	})

	// /api/new?since=&limit= —— 时间窗内「首次出现」的应用通道，按 App 归组 + 观测覆盖。
	// **不收过滤切片**（见 CONTEXT「过滤切片」的边界）：首次出现要拿窗口外的历史当参照系，
	// 而切片只会缩小可见范围，套上去会让 min(ts) 漂移成「在该切片内首次出现的时刻」。
	app.Get("/api/new", func(c *fiber.Ctx) error {
		since, err := parseDur(c.Query("since"))
		if err != nil {
			return badReq(c, err)
		}
		limit := c.QueryInt("limit", 0)
		chs, err := st.NewChannels(since, limit)
		if err != nil {
			return svrErr(c, err)
		}
		cov, err := st.Coverage()
		if err != nil {
			return svrErr(c, err)
		}
		return c.JSON(newResponse{
			Apps:     groupNewChannels(chs, mutedProcs, classifier),
			Coverage: cov,
			// 结果撞到上限 → 如实告知被截断。静默截断会让「只列了这些」被读成「只有这些」。
			Truncated: len(chs) >= store.EffectiveNewChannelsLimit(limit),
		})
	})

	// /api/trackers?<过滤> —— 切片内「多少连接走了已知追踪器」+ 按归属公司 top-N（limit 默认 10）。
	// host→归属 归类在 Go 侧做（DuckDB 无公共后缀函数）：取全量 host 计数 → 归类器归并。未拉取数据 → 全非追踪器。
	app.Get("/api/trackers", func(c *fiber.Ctx) error {
		f, err := parseFilter(c)
		if err != nil {
			return badReq(c, err)
		}
		hosts, err := st.HostCounts(f)
		if err != nil {
			return svrErr(c, err)
		}
		return c.JSON(computeTrackerBreakdown(hosts, classifier, c.QueryInt("limit", 8)))
	})

	// 未知 /api/* 返回 404 JSON（而非落到静态回退的 index.html，避免 client 把 HTML 当 JSON）。
	app.Use("/api", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未知接口 " + c.Path()})
	})
}

// newChannelDTO 是一条新增应用通道对外的形态：store 的事实 + 追踪器归属（Go 侧归类，见 /api/trackers）。
type newChannelDTO struct {
	store.NewChannel
	Tracker string `json:"tracker"` // 归属公司；空 = 未命中已知追踪器或未拉取追踪器数据
}

// newAppGroup 是按 App 归组后的新增：该 App 新增了几条通道、是否属于「用户指定目的地的进程」。
type newAppGroup struct {
	Process  string          `json:"process"`
	Count    int             `json:"count"`
	Muted    bool            `json:"muted"`
	Channels []newChannelDTO `json:"channels"`
}

// newResponse 是 /api/new 的返回：新增分组 + 观测覆盖 + 是否被上限截断。
// Coverage 与结论同行返回而非另开接口——「首次出现」只在观测覆盖范围内成立，
// 让调用方能够只取结论、不看它的证据基础，本身就是个坑（ADR-0014）。
type newResponse struct {
	Apps      []newAppGroup  `json:"apps"`
	Coverage  store.Coverage `json:"coverage"`
	Truncated bool           `json:"truncated"`
}

// groupNewChannels 把扁平的新增通道按 App 归组，标注 muted 与追踪器归属。
// 排序：未折叠的在前，组内按新增数降序——被折叠的沉底但**保留**，用户随时能展开
// （只标记不删除：默认清单是保守猜测，猜错了不该让证据消失）。
// 入参 chs 已按 (process, 首次时刻倒序) 排好，故组内顺序直接沿用、无需再排。
func groupNewChannels(chs []store.NewChannel, muted []string, cl *tracker.Classifier) []newAppGroup {
	idx := make(map[string]int, len(chs))
	groups := []newAppGroup{}
	for _, ch := range chs {
		i, ok := idx[ch.Process]
		if !ok {
			i = len(groups)
			idx[ch.Process] = i
			groups = append(groups, newAppGroup{
				Process:  ch.Process,
				Muted:    isMutedProcess(ch.Process, muted),
				Channels: []newChannelDTO{},
			})
		}
		owner := ""
		if cl != nil {
			if o, known := cl.Classify(ch.Host); known {
				owner = o
			}
		}
		groups[i].Channels = append(groups[i].Channels, newChannelDTO{NewChannel: ch, Tracker: owner})
		groups[i].Count++
	}
	sort.SliceStable(groups, func(a, b int) bool {
		if groups[a].Muted != groups[b].Muted {
			return !groups[a].Muted // 未折叠的排前面
		}
		return groups[a].Count > groups[b].Count
	})
	return groups
}

// ruleGap 是一条「规则缺口」（见 CONTEXT 术语）：一个可注册域 + 它的兜底汇总 = 一条待补的分流规则。
// Hosts 保留其下的具体子域，供前端展开——写宽泛规则前得能核实它到底盖住了什么。
type ruleGap struct {
	Domain string                  `json:"domain"`
	Conns  int64                   `json:"conns"`
	Bytes  int64                   `json:"bytes"`
	LastTS time.Time               `json:"lastTs"`
	Hosts  []store.FallthroughHost `json:"hosts"`
}

// gapsResponse 是 /api/gaps 的返回。
// Total* 是**全部**兜底连接（含 IP 目标）的合计，供前端算「累计覆盖」——分母含 IP 区，
// 故域名列表的累计值到底也不会满 100%，剩下的正是 IP 区那部分，这是诚实的。
type gapsResponse struct {
	Gaps       []ruleGap               `json:"gaps"`
	IPTargets  []store.FallthroughHost `json:"ipTargets"`
	Bypassed   int64                   `json:"bypassed"`
	TotalConns int64                   `json:"totalConns"`
	TotalBytes int64                   `json:"totalBytes"`
}

// groupRuleGaps 把逐 host 的兜底明细折叠成「规则缺口」：按可注册域归组，
// 取不到可注册域的（IP 字面量、单标签）单独归入 IP 目标——它们写不了域名规则。
//
// 折叠必须在 Go 侧：DuckDB 没有公共后缀函数（同 HostCounts 供追踪器归类的既有路子）。
// 排序也在折叠之后才有意义：按字节降序，字节相同再按连接数，最后按名字定序（结果稳定可测）。
func groupRuleGaps(hosts []store.FallthroughHost) ([]ruleGap, []store.FallthroughHost) {
	idx := make(map[string]int, len(hosts))
	gaps := []ruleGap{}
	ips := []store.FallthroughHost{}

	for _, h := range hosts {
		domain, ok := tracker.RegistrableDomain(h.Host)
		if !ok {
			ips = append(ips, h)
			continue
		}
		i, seen := idx[domain]
		if !seen {
			i = len(gaps)
			idx[domain] = i
			gaps = append(gaps, ruleGap{Domain: domain, Hosts: []store.FallthroughHost{}})
		}
		g := &gaps[i]
		g.Conns += h.Conns
		g.Bytes += h.Bytes
		g.Hosts = append(g.Hosts, h)
		if h.LastTS.After(g.LastTS) {
			g.LastTS = h.LastTS // 组的「最后兜底」取组内最晚的那次
		}
	}

	byWeight := func(ac, ab int64, an string, bc, bb int64, bn string) bool {
		if ab != bb {
			return ab > bb
		}
		if ac != bc {
			return ac > bc
		}
		return an < bn
	}
	sort.SliceStable(gaps, func(a, b int) bool {
		return byWeight(gaps[a].Conns, gaps[a].Bytes, gaps[a].Domain,
			gaps[b].Conns, gaps[b].Bytes, gaps[b].Domain)
	})
	for i := range gaps {
		hs := gaps[i].Hosts
		sort.SliceStable(hs, func(a, b int) bool {
			return byWeight(hs[a].Conns, hs[a].Bytes, hs[a].Host, hs[b].Conns, hs[b].Bytes, hs[b].Host)
		})
	}
	sort.SliceStable(ips, func(a, b int) bool {
		return byWeight(ips[a].Conns, ips[a].Bytes, ips[a].Host, ips[b].Conns, ips[b].Bytes, ips[b].Host)
	})
	return gaps, ips
}
