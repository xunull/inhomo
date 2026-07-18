package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/spf13/cobra"
	"github.com/xunull/inhomo/internal/store"
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
	addr, _ := cmd.Flags().GetString(flagAddr)
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
	// Ctrl-C（ctx 取消）→ 关闭 Fiber，app.Listen 随即返回。
	go func() {
		<-ctx.Done()
		_ = app.ShutdownWithContext(context.Background())
	}()

	fmt.Fprintf(os.Stderr, "[inhomo] Web 分析接口：http://%s/api/summary （Ctrl-C 停）\n", addr)
	listenErr := app.Listen(addr)

	stop() // 确保记录 goroutine 收尾（正常关闭时 ctx 已取消，此处幂等）
	recordErr := <-recErr
	if listenErr != nil {
		return listenErr // Listen 出错（如端口占用）优先
	}
	return recordErr
}

// registerRoutes 注册 Web 分析接口。handler 薄：调 store 查询 + 编码 JSON。
func registerRoutes(app *fiber.App, st *store.Store) {
	app.Get("/api/summary", func(c *fiber.Ctx) error {
		sm, err := st.Summary()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(sm)
	})
}
