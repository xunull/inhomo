// Package sink 负责把明文 HTTP 泄露事件落盘为 JSONL（原始层，一条不漏）。
// Record 的构造与序列化是纯逻辑，可对着内存 Writer 测试；真实文件写入由调用方接线。
package sink

import (
	"encoding/json"
	"io"
	"time"

	"github.com/xunull/inhomo/internal/detect"
)

// Record 是 JSONL 文件里的一行。
type Record struct {
	TS     string `json:"ts"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Node   string `json:"node"`
	Region string `json:"region"`
	Rule   string `json:"rule"`
}

// tsLayout 是 JSONL 时间戳格式：RFC3339 + 毫秒，便于同一秒内高频事件的排序。
const tsLayout = "2006-01-02T15:04:05.000Z07:00"

// NewRecord 从泄露事件与观测时刻构造 Record（纯函数）。
func NewRecord(leak detect.LeakEvent, t time.Time) Record {
	return Record{
		TS:     t.Format(tsLayout),
		Host:   leak.Host,
		Port:   leak.Port,
		Node:   leak.Node,
		Region: leak.Region,
		Rule:   leak.Rule,
	}
}

// JSONLWriter 把每个泄露事件作为一行 JSON 追加写入底层 Writer。
type JSONLWriter struct {
	w io.Writer
}

// NewJSONLWriter 包装一个底层 Writer（真实场景传 *os.File，测试传 *bytes.Buffer）。
func NewJSONLWriter(w io.Writer) *JSONLWriter {
	return &JSONLWriter{w: w}
}

// Write 把一条 Record 序列化为一行 JSON（末尾换行）直接写入底层 Writer。
// 不做用户态缓冲——每条即落地，保证「原始层一条不漏」，且优雅退出时无需额外 flush。
func (j *JSONLWriter) Write(r Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = j.w.Write(b)
	return err
}
