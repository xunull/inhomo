package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/xunull/inhomo/internal/logstream"
)

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "原样查看 mihomo 日志（逐行 payload + 级别标记）",
		Args:  cobra.NoArgs,
		RunE:  runLogs,
	}
	cmd.Flags().String(flagLevel, "info", "订阅的日志级别：info / warning / error / debug")
	return cmd
}

func runLogs(cmd *cobra.Command, _ []string) error {
	// SIGINT / SIGTERM 触发优雅退出。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client, level := newClient(cmd)
	client.OnConnect = func() {
		fmt.Fprintln(os.Stderr, "[inhomo] 已连接 /logs，原样输出（Ctrl-C 退出）…")
	}
	fmt.Fprintf(os.Stderr, "[inhomo] 连接 %s 的 /logs?level=%s …\n", client.BaseURL, level)

	return client.Run(ctx, level, func(msg logstream.LogMessage) {
		fmt.Println(formatLogLine(msg))
	})
}

// formatLogLine 把一条日志渲染成「[级别] payload」：payload 是 mihomo 日志原文，
// 前面加个级别小标记便于区分 warning/error；不加时间戳（API 本就不给）。
func formatLogLine(msg logstream.LogMessage) string {
	return "[" + levelTag(msg.Type) + "] " + msg.Payload
}

// levelTag 把 /logs 的 type 归一化成简短级别标记。
func levelTag(t string) string {
	switch t {
	case "warning":
		return "warn"
	case "error":
		return "err"
	case "":
		return "?"
	default:
		return t // info / debug 原样
	}
}
