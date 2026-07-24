package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/xunull/inhomo/internal/tracker"
)

// newTrackerCmd 管理「追踪器识别数据」——DuckDuckGo Tracker Radar 的域名→归属表。
// 其数据是 CC BY-NC-SA 4.0，不随二进制分发，改由本命令运行期拉到 ~/.inhomo/ 后离线比对（见 ADR-0011）。
func newTrackerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tracker",
		Short: "管理追踪器识别数据（DuckDuckGo Tracker Radar，运行期拉取）",
	}
	cmd.AddCommand(newTrackerUpdateCmd())
	return cmd
}

func newTrackerUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "拉取/更新追踪器域名表到 ~/.inhomo/tracker-radar.json（audit 据此标注已知追踪器）",
		Args:  cobra.NoArgs,
		RunE:  runTrackerUpdate,
	}
}

func runTrackerUpdate(_ *cobra.Command, _ []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("无法确定 home 目录：%w", err)
	}
	dest := tracker.CachePath(home)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "[inhomo] 拉取追踪器数据 %s …\n", tracker.SourceURL)
	if err := tracker.Fetch(ctx, tracker.SourceURL, dest); err != nil {
		return fmt.Errorf("拉取失败：%w", err)
	}
	c, err := tracker.Load(dest)
	if err != nil {
		return fmt.Errorf("校验下载的数据失败：%w", err)
	}
	fmt.Fprintf(os.Stderr, "[inhomo] 已写入 %s（%d 个已知追踪器域名）\n", dest, c.Len())
	return nil
}
