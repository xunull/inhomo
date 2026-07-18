package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/xunull/inhomo/internal/detect"
	"github.com/xunull/inhomo/internal/logstream"
	"github.com/xunull/inhomo/internal/store"
)

func newRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record",
		Short: "把每条连接事件（全量，不止泄露）写入嵌入式 DuckDB，供分析统计",
		Args:  cobra.NoArgs,
		RunE:  runRecord,
	}
	cmd.Flags().String(flagLevel, "info", "订阅的日志级别；连接日志需要 info")
	cmd.Flags().String("db", "", "DuckDB 库文件路径（默认 ~/.inhomo/connections.duckdb）")
	return cmd
}

// resolveDBPath 计算最终库路径：空 → 默认 ~/.inhomo/connections.duckdb；"~/" 前缀 → 展开到 home。
func resolveDBPath(p, home string) string {
	switch {
	case p == "":
		return filepath.Join(home, ".inhomo", "connections.duckdb")
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	default:
		return p
	}
}

func runRecord(cmd *cobra.Command, _ []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("无法确定 home 目录：%w", err)
	}
	dbFlag, _ := cmd.Flags().GetString("db")
	dbPath := resolveDBPath(dbFlag, home)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("创建目录 %s：%w", filepath.Dir(dbPath), err)
	}

	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close() // 退出前落地剩余缓冲
	fmt.Fprintf(os.Stderr, "[inhomo] 连接事件将写入 DuckDB：%s\n", dbPath)

	// SIGINT / SIGTERM 触发优雅退出。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 定期 flush，让 appender 缓冲落地、数据可查（关闭后 Flush 为无操作）。
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := st.Flush(); err != nil {
					fmt.Fprintf(os.Stderr, "[inhomo] flush 失败：%v\n", err)
				}
			}
		}
	}()

	client, level := newClient(cmd, "已连接 /logs，开始记录连接事件…")

	const statsInterval = 10 * time.Second
	var recorded, skipped int
	var lastStats time.Time
	err = client.Run(ctx, level, func(msg logstream.LogMessage) {
		cl, ok := detect.Parse(msg.Payload)
		if !ok {
			skipped++
			return
		}
		if err := st.Add(store.Event{
			TS:      time.Now(),
			Process: cl.Process,
			Network: cl.Network,
			Host:    cl.Host,
			Port:    cl.Port,
			Rule:    cl.Rule,
			Node:    cl.Node,
			Region:  detect.Region(cl.Node),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "[inhomo] 写入失败：%v\n", err)
			return
		}
		recorded++
		if now := time.Now(); now.Sub(lastStats) >= statsInterval {
			fmt.Fprintf(os.Stderr, "[inhomo] 已记录 %d 条连接 / 跳过 %d 行\n", recorded, skipped)
			lastStats = now
		}
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[inhomo] 已停止；共记录 %d 条连接 / 跳过 %d 行\n", recorded, skipped)
	return nil
}
