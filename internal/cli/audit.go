package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/xunull/inhomo/internal/aggregate"
	"github.com/xunull/inhomo/internal/detect"
	"github.com/xunull/inhomo/internal/logstream"
	"github.com/xunull/inhomo/internal/sink"
	"github.com/xunull/inhomo/internal/tracker"
)

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "识别并记录「明文 HTTP + 经出境节点」的泄露事件",
		Args:  cobra.NoArgs,
		RunE:  runAudit,
	}
	cmd.Flags().String(flagLevel, "info", "订阅的日志级别；连接日志需要 info")
	cmd.Flags().String("http-ports", "80", "视为明文 HTTP 的目的端口集（逗号分隔）")
	cmd.Flags().String("out", "", "JSONL 输出文件路径（留空则只打印到终端、不落盘）")
	cmd.Flags().Duration("window", 5*time.Minute, "终端聚合时间窗：同一(节点,host)在窗内只冒一次")
	return cmd
}

func runAudit(cmd *cobra.Command, _ []string) error {
	v := cfgOf(cmd)
	httpPortsFlag := v.GetString("http-ports")
	outPath := v.GetString("out")
	window := v.GetDuration("window")

	httpPorts := parseHTTPPorts(httpPortsFlag)

	// 可选 JSONL 落盘：每个泄露事件追加写一行（原始层，一条不漏）。
	var writer *sink.JSONLWriter
	if outPath != "" {
		f, err := os.OpenFile(outPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("无法打开输出文件 %s：%w", outPath, err)
		}
		defer f.Close()
		writer = sink.NewJSONLWriter(f)
		fmt.Fprintf(os.Stderr, "[inhomo] 泄露事件将追加写入 %s\n", outPath)
	}

	// 追踪器识别数据（运行期 `inhomo tracker update` 拉取）：加载失败/未拉取 → 空归类器，泄露行不标注、不报错。
	home, _ := os.UserHomeDir() // 取不到 home 时 Load 落到不存在路径 → 空归类器，正是要的降级，故有意丢弃此错误
	classifier, loadErr := tracker.Load(tracker.CachePath(home))
	switch {
	case loadErr != nil:
		fmt.Fprintf(os.Stderr, "[inhomo] 追踪器数据损坏，忽略（%v）；跑 `inhomo tracker update` 可重拉\n", loadErr)
		classifier = &tracker.Classifier{}
	case classifier.Len() == 0:
		fmt.Fprintln(os.Stderr, "[inhomo] 未加载追踪器数据（跑 `inhomo tracker update` 后可在泄露行标注已知追踪器）")
	}

	// 终端聚合器：按 (节点,host) 时间窗去重，只冒一次不刷屏。
	agg := aggregate.New(window)

	// SIGINT / SIGTERM 触发优雅退出（取消 ctx，Run 随即返回）。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client, level := newClient(cmd, "已连接 /logs，开始识别明文 HTTP 泄露…")

	// skipped：非连接日志或无法解析的行计数（Parse/Classify 在 Run 的同一 goroutine 里被同步调用，无并发）。
	const statsInterval = 10 * time.Second
	var skipped, totalLeaks int
	var lastStats time.Time
	printStats := func(lead string) {
		fmt.Fprintf(os.Stderr, "[inhomo] %s累计 %d 泄露 / %d 个(节点,host)组合 / 跳过 %d 行\n",
			lead, totalLeaks, agg.Distinct(), skipped)
	}

	handle := func(msg logstream.LogMessage) {
		cl, ok := detect.Parse(msg.Payload)
		if !ok {
			skipped++
			return
		}
		leak, isLeak := detect.Classify(cl, httpPorts)
		if !isLeak {
			return
		}
		now := time.Now()
		totalLeaks++

		// 原始层：每个事件都写 JSONL（一条不漏，不受终端聚合影响）。
		if writer != nil {
			if err := writer.Write(sink.NewRecord(leak, now)); err != nil {
				fmt.Fprintf(os.Stderr, "[inhomo] 写 JSONL 失败：%v\n", err)
			}
		}

		// 可用层：按 (节点,host) 聚合，终端只冒一次、不刷屏。
		if emit, suppressed := agg.Observe(aggregate.Key{Node: leak.Node, Host: leak.Host}, now); emit {
			fmt.Println(formatLeakLine(now, leak, suppressed, window, classifier))
		}

		// 运行统计：按时间节流刷新（实时可见，低流量也不会久无输出）。
		if now.Sub(lastStats) >= statsInterval {
			printStats("")
			lastStats = now
		}
	}

	if err := client.Run(ctx, level, handle); err != nil {
		return err
	}
	printStats("已停止；")
	return nil
}

// parseHTTPPorts 把逗号分隔的端口串解析成集合；解析不出任何有效端口则兜底为 {80}。
func parseHTTPPorts(s string) map[int]bool {
	m := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		if p, ok := detect.ParsePort(part); ok {
			m[p] = true
		}
	}
	if len(m) == 0 {
		m[80] = true
	}
	return m
}

// formatLeakLine 渲染一条终端泄露行；命中已知追踪器时附归属标注；suppressed>0 时附一窗被抑制的次数。
func formatLeakLine(now time.Time, leak detect.LeakEvent, suppressed int, window time.Duration, c *tracker.Classifier) string {
	line := fmt.Sprintf("%s  明文HTTP泄露  %s:%d  →  %s [%s]  规则:%s",
		now.Format("15:04:05"), leak.Host, leak.Port, leak.Node, leak.Region, leak.Rule)
	line += trackerNote(c, leak.Host)
	if suppressed > 0 {
		line += fmt.Sprintf("  (过去 %s 内又 ×%d)", window, suppressed)
	}
	return line
}

// trackerNote 渲染泄露行尾的追踪器标注：已知 → "  [已知追踪器 · <归属>]"（无归属名时省略公司），未知 → ""。
func trackerNote(c *tracker.Classifier, host string) string {
	owner, known := c.Classify(host)
	if !known {
		return ""
	}
	if owner == "" {
		return "  [已知追踪器]"
	}
	return "  [已知追踪器 · " + owner + "]"
}
