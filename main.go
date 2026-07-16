// inhomo：明文 HTTP 出境泄露审计守护进程。
//
// 明文 HTTP 出境泄露审计（MVP，工单 T01–T04）：订阅 mihomo /logs 流，实时识别
// 「明文 HTTP 泄露事件」；原始层把每个事件追加写入 JSONL（-out，一条不漏），
// 可用层在终端按 (节点,host) 时间窗聚合、只冒一次不刷屏，并实时显示运行统计。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/xunull/inhomo/internal/aggregate"
	"github.com/xunull/inhomo/internal/detect"
	"github.com/xunull/inhomo/internal/logstream"
	"github.com/xunull/inhomo/internal/sink"
)

func main() {
	controller := flag.String("controller", "127.0.0.1:9090", "mihomo external-controller 地址（host:port，可带 http:// 前缀）")
	secret := flag.String("secret", "", "external-controller 的 secret（未开启鉴权则留空）")
	level := flag.String("level", "info", "订阅的日志级别；连接日志需要 info")
	httpPortsFlag := flag.String("http-ports", "80", "视为明文 HTTP 的目的端口集（逗号分隔）")
	outPath := flag.String("out", "", "JSONL 输出文件路径（留空则只打印到终端、不落盘）")
	window := flag.Duration("window", 5*time.Minute, "终端聚合时间窗：同一(节点,host)在窗内只冒一次")
	flag.Parse()

	httpPorts := parseHTTPPorts(*httpPortsFlag)

	// 可选 JSONL 落盘：每个泄露事件追加写一行（原始层，一条不漏）。
	var writer *sink.JSONLWriter
	if *outPath != "" {
		f, err := os.OpenFile(*outPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[inhomo] 无法打开输出文件 %s：%v\n", *outPath, err)
			os.Exit(1)
		}
		defer f.Close()
		writer = sink.NewJSONLWriter(f)
		fmt.Fprintf(os.Stderr, "[inhomo] 泄露事件将追加写入 %s\n", *outPath)
	}

	// 终端聚合器：按 (节点,host) 时间窗去重，只冒一次不刷屏。
	agg := aggregate.New(*window)

	// SIGINT / SIGTERM 触发优雅退出（取消 ctx，Run 随即返回）。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := logstream.New(*controller, *secret)
	client.OnConnect = func() {
		fmt.Fprintln(os.Stderr, "[inhomo] 已连接 /logs，开始识别明文 HTTP 泄露…")
	}
	client.OnReconnect = func(wait time.Duration) {
		fmt.Fprintf(os.Stderr, "[inhomo] 连接断开，%s 后重连…\n", wait)
	}

	// 已核对 mihomo log 包：事件先无条件入 logCh 再按全局 level 决定是否打到 stdout，
	// 故 /logs?level=info 的订阅与 mihomo 全局 log-level 无关，无需用户改配置。
	fmt.Fprintf(os.Stderr, "[inhomo] 连接 %s 的 /logs?level=%s …（mihomo 按此级别推送日志，与其全局 log-level 设置无关）\n",
		client.BaseURL, *level)

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
			fmt.Println(formatLeakLine(now, leak, suppressed, *window))
		}

		// 运行统计：按时间节流刷新（实时可见，低流量也不会久无输出）。
		if now.Sub(lastStats) >= statsInterval {
			printStats("")
			lastStats = now
		}
	}

	err := client.Run(ctx, *level, handle)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[inhomo] 退出：%v\n", err)
		os.Exit(1)
	}
	printStats("已停止；")
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

// formatLeakLine 渲染一条终端泄露行；suppressed>0 时附上一窗被抑制（未显示）的次数。
func formatLeakLine(now time.Time, leak detect.LeakEvent, suppressed int, window time.Duration) string {
	line := fmt.Sprintf("%s  明文HTTP泄露  %s:%d  →  %s [%s]  规则:%s",
		now.Format("15:04:05"), leak.Host, leak.Port, leak.Node, leak.Region, leak.Rule)
	if suppressed > 0 {
		line += fmt.Sprintf("  (过去 %s 内又 ×%d)", window, suppressed)
	}
	return line
}
