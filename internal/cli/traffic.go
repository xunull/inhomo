package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/xunull/inhomo/internal/detect"
	"github.com/xunull/inhomo/internal/logstream"
	"github.com/xunull/inhomo/internal/store"
)

// activeConn 是一条活跃连接的最小追踪状态（从 /connections 的 Conn 抽出）。
type activeConn struct {
	start   time.Time
	up      int64
	down    int64
	process string
	network string
	host    string
	node    string
	region  string
	port    int
}

// toActive 把 /connections 的一条连接转成追踪状态：node 取 chains[0]（实际出境节点），
// region 从 node 名解析（与现有一致），port 由字符串端口解析。
func toActive(c logstream.Conn) activeConn {
	node := ""
	if len(c.Chains) > 0 {
		node = c.Chains[0]
	}
	port, _ := strconv.Atoi(c.Metadata.DestinationPort)
	return activeConn{
		start:   c.Start,
		up:      c.Upload,
		down:    c.Download,
		process: c.Metadata.Process,
		network: c.Metadata.Network,
		host:    c.Metadata.Host,
		node:    node,
		region:  detect.Region(node),
		port:    port,
	}
}

// toRecord 把追踪状态在 now 时刻定格为一条流量记录（时长≈now−start）。
func (a activeConn) toRecord(now time.Time) store.TrafficRecord {
	return store.TrafficRecord{
		Start:      a.start,
		Process:    a.process,
		Network:    a.network,
		Host:       a.host,
		Port:       a.port,
		Node:       a.node,
		Region:     a.region,
		UpBytes:    a.up,
		DownBytes:  a.down,
		DurationMs: now.Sub(a.start).Milliseconds(),
	}
}

// completedFrom 找出 prev 中已从 cur 消失（连接已关闭）的 id，用最后一次快照的字节产出流量记录。
// 短连接（开+关都在两次轮询之间、从未进过快照）不会出现在 prev，故不会产出——这是采样的固有取舍。
func completedFrom(prev, cur map[string]activeConn, now time.Time) []store.TrafficRecord {
	var recs []store.TrafficRecord
	for id, a := range prev {
		if _, ok := cur[id]; !ok {
			recs = append(recs, a.toRecord(now))
		}
	}
	return recs
}

// pollTraffic 周期轮询 /connections，按 id 追踪，连接消失时把「流量记录」写入 st；阻塞到 ctx 取消。
// interval<=0 则不启用。ctx 取消时把仍活跃的连接也落一次（避免长活连接完全丢失）。
func pollTraffic(ctx context.Context, client *logstream.Client, st *store.Store, interval time.Duration) {
	if interval <= 0 {
		return
	}
	record := func(recs []store.TrafficRecord) {
		for _, r := range recs {
			if err := st.AddTraffic(r); err != nil {
				fmt.Fprintf(os.Stderr, "[inhomo] 写流量记录失败：%v\n", err)
			}
		}
	}

	t := time.NewTicker(interval)
	defer t.Stop()
	prev := map[string]activeConn{}
	for {
		select {
		case <-ctx.Done():
			// 退出：把仍活跃的连接也落一次。
			record(completedFrom(prev, map[string]activeConn{}, time.Now()))
			return
		case <-t.C:
			snap, err := client.FetchConnections(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				fmt.Fprintf(os.Stderr, "[inhomo] 拉 /connections 失败：%v\n", err)
				continue
			}
			cur := make(map[string]activeConn, len(snap.Connections))
			for _, c := range snap.Connections {
				cur[c.ID] = toActive(c)
			}
			record(completedFrom(prev, cur, time.Now()))
			prev = cur
		}
	}
}
