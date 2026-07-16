// inhomo：明文 HTTP 出境泄露审计守护进程。
//
// 已实现到工单 T02：订阅 mihomo /logs 流，实时识别「明文 HTTP 泄露事件」
// （明文 HTTP 连接 + 经出境节点中转）并打印到终端；非泄露、非连接日志跳过。
// 落盘（T03）与终端聚合摘要（T04）为后续工单。
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

	"github.com/xunull/inhomo/internal/detect"
	"github.com/xunull/inhomo/internal/logstream"
)

func main() {
	controller := flag.String("controller", "127.0.0.1:9090", "mihomo external-controller 地址（host:port，可带 http:// 前缀）")
	secret := flag.String("secret", "", "external-controller 的 secret（未开启鉴权则留空）")
	level := flag.String("level", "info", "订阅的日志级别；连接日志需要 info")
	httpPortsFlag := flag.String("http-ports", "80", "视为明文 HTTP 的目的端口集（逗号分隔）")
	flag.Parse()

	httpPorts := parseHTTPPorts(*httpPortsFlag)

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
	var skipped int
	handle := func(msg logstream.LogMessage) {
		ev, ok := detect.Parse(msg.Payload)
		if !ok {
			skipped++
			return
		}
		leak, isLeak := detect.Classify(ev, httpPorts)
		if !isLeak {
			return
		}
		fmt.Printf("%s  明文HTTP泄露  %s:%d  →  %s [%s]  规则:%s\n",
			time.Now().Format("15:04:05"), leak.Host, leak.Port, leak.Node, leak.Region, leak.Rule)
	}

	err := client.Run(ctx, *level, handle)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[inhomo] 退出：%v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[inhomo] 已停止；本次跳过 %d 行（非连接日志或无法解析）\n", skipped)
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
