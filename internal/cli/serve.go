package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/spf13/cobra"
	"github.com/xunull/inhomo/internal/store"
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
	cmd.Flags().String(flagAddr, "127.0.0.1:8464", "Web 服务监听地址（默认仅本机、无鉴权）")
	return cmd
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

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	registerRoutes(app, st)
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
func registerRoutes(app *fiber.App, st *store.Store) {
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

	// 未知 /api/* 返回 404 JSON（而非落到静态回退的 index.html，避免 client 把 HTML 当 JSON）。
	app.Use("/api", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "未知接口 " + c.Path()})
	})
}
