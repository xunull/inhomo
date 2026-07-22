package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCmd 是 `inhomo version` 子命令：打印版本号（与 `inhomo --version` 同源同值）。
//
// 它用 no-op PersistentPreRunE 覆盖 root 的 bindConfig——查版本不该依赖配置：即便
// ~/.inhomo/config.yaml 缺失或格式错，`inhomo version` 也要照常报版本（用于排查环境）。
// 这是**唯一**有意覆盖 bindConfig 的命令；覆盖安全，因为它不调 cfgOf（cfgOf 在别处对
// 「绕过 bindConfig 却仍取配置」才 panic，见 config.go）。
func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:               "version",
		Short:             "打印 inhomo 版本号",
		Args:              cobra.NoArgs,
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "inhomo version %s\n", version)
			return err
		},
	}
}
