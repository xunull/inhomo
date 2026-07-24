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

// loadClassifier 从默认缓存加载追踪器归类器，供 audit / serve 共用。集中处理告警（互斥、只一条）：
// 数据损坏 → 提示重拉并退化为空；缺失/空 → 提示可拉取；正常 → 静默。出错/缺失都返回空归类器（全 unknown、不阻塞）。
func loadClassifier() *tracker.Classifier {
	home, _ := os.UserHomeDir() // 取不到 home 时 Load 落到不存在路径 → 空归类器，正是要的降级
	c, err := tracker.Load(tracker.CachePath(home))
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "[inhomo] 追踪器数据损坏，忽略（%v）；跑 `inhomo tracker update` 可重拉\n", err)
		return &tracker.Classifier{}
	case c.Len() == 0:
		fmt.Fprintln(os.Stderr, "[inhomo] 未加载追踪器数据（跑 `inhomo tracker update` 后可标注/统计已知追踪器）")
	}
	return c
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
