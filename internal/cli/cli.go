// Package cli 用 cobra 组织 inhomo 的子命令。
// 连接参数 --controller / --secret 是 root 持久 flag，跨子命令共享（见 ADR-0003）。
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// 持久 flag 名（cli.go 注册、各子命令读取，避免跨文件字符串硬耦合）。
const (
	flagController = "controller"
	flagSecret     = "secret"
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
	return root
}

// Execute 是二进制入口：解析参数并运行对应子命令；出错时以 [inhomo] 前缀打印并退出 1。
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "[inhomo]", err)
		os.Exit(1)
	}
}
