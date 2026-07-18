// Package cli 用 cobra 组织 inhomo 的子命令。
// 连接参数 --controller / --secret 是 root 持久 flag，跨子命令共享（见 ADR-0003）。
package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/xunull/inhomo/internal/logstream"
)

// flag 名常量（跨文件注册/读取，避免字符串硬耦合）。controller/secret 是 root 持久 flag；
// level 是各子命令各自注册的同名 flag。
const (
	flagController = "controller"
	flagSecret     = "secret"
	flagLevel      = "level"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "inhomo",
		Short:         "审计经由 mihomo 出站的明文 HTTP 泄露",
		Long:          "inhomo 订阅 mihomo 的 /logs 流，审计经由代理出站的明文 HTTP 泄露。",
		SilenceUsage:  true, // 运行期错误不再叠加 usage
		SilenceErrors: true, // 错误由 Execute 统一以 [inhomo] 前缀打印
	}
	root.PersistentFlags().String(flagController, "127.0.0.1:9090",
		"mihomo external-controller：TCP 如 127.0.0.1:9090，或 Unix socket 如 unix:///tmp/verge/verge-mihomo.sock")
	root.PersistentFlags().String(flagSecret, "", "external-controller 的 secret（未开启鉴权则留空）")

	root.AddCommand(newAuditCmd())
	root.AddCommand(newLogsCmd())
	return root
}

// Execute 是二进制入口：解析参数并运行对应子命令；出错时以 [inhomo] 前缀打印并退出 1。
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "[inhomo]", err)
		os.Exit(1)
	}
}

// newClient 按命令的 flag 建好 logstream 客户端、装上通用的重连提示，并返回订阅级别。
// 各子命令自行设置 OnConnect 与启动提示（文案不同），共享连接与重连逻辑。
func newClient(cmd *cobra.Command) (*logstream.Client, string) {
	controller, _ := cmd.Flags().GetString(flagController)
	secret, _ := cmd.Flags().GetString(flagSecret)
	level, _ := cmd.Flags().GetString(flagLevel)

	client := logstream.New(controller, secret)
	client.OnReconnect = func(wait time.Duration) {
		fmt.Fprintf(os.Stderr, "[inhomo] 连接断开，%s 后重连…\n", wait)
	}
	return client, level
}
