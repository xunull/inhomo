// inhomo：明文 HTTP 出境泄露审计守护进程。
//
// 当前实现到工单 T01「走通骨架」：连上 mihomo external-controller 的 /logs 流，
// 把原始日志 payload 实时回显到终端；断线自动退避重连；启动时对不可达/鉴权失败
// 给出清晰提示。后续工单（T02 起）在此基础上叠加解析、判定、落盘与聚合。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xunull/inhomo/internal/logstream"
)

func main() {
	// 命令行配置：mihomo external-controller 的地址与 secret，以及订阅的日志级别。
	controller := flag.String("controller", "127.0.0.1:9090", "mihomo external-controller 地址（host:port，可带 http:// 前缀）")
	secret := flag.String("secret", "", "external-controller 的 secret（未开启鉴权则留空）")
	level := flag.String("level", "info", "订阅的日志级别；连接日志需要 info")
	flag.Parse()

	// SIGINT / SIGTERM 触发优雅退出（取消 ctx，Run 随即返回）。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := logstream.New(*controller, *secret)
	client.OnConnect = func() {
		fmt.Fprintln(os.Stderr, "[inhomo] 已连接 /logs，开始接收日志…")
	}
	client.OnReconnect = func(wait time.Duration) {
		fmt.Fprintf(os.Stderr, "[inhomo] 连接断开，%s 后重连…\n", wait)
	}

	// 启动提示：已核对 mihomo log 包——事件先无条件入 logCh 再按全局 level 决定是否打到 stdout，
	// 故 /logs?level=info 的订阅端与 mihomo 全局 log-level 无关，照样收到 info 级连接日志，无需用户改配置。
	fmt.Fprintf(os.Stderr, "[inhomo] 连接 %s 的 /logs?level=%s …（mihomo 按此级别推送日志，与其全局 log-level 设置无关）\n",
		client.BaseURL, *level)

	// T01 只把原始 payload 打到 stdout；判定/落盘在后续工单接入。
	err := client.Run(ctx, *level, func(msg logstream.LogMessage) {
		fmt.Println(msg.Payload)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[inhomo] 退出：%v\n", err)
		os.Exit(1)
	}
}
