// Package store 把「连接事件」写入嵌入式 DuckDB（record 用）。
// 写入走 DuckDB 的 Appender（批量、高效）；Add/Flush/Close 用一把锁串行化，
// 并以 closed 标志防止 use-after-close（record 里 Add 与定时 Flush、退出 Close 来自不同 goroutine）。
package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/marcboeker/go-duckdb/v2"
)

const schema = `CREATE TABLE IF NOT EXISTS connections (
	ts        TIMESTAMP,
	process   VARCHAR,
	network   VARCHAR,
	host      VARCHAR,
	port      INTEGER,
	rule      VARCHAR,
	node      VARCHAR,
	region    VARCHAR
)`

// Event 是一条连接事件（记录单位，对应 CONTEXT 的「连接事件」）。
type Event struct {
	TS      time.Time
	Process string
	Network string
	Host    string
	Port    int
	Rule    string
	Node    string
	Region  string
}

// Store 是 DuckDB 上的连接事件写入器（同时持有可查询的 *sql.DB，供后续 Web/报告用）。
type Store struct {
	connector *duckdb.Connector
	db        *sql.DB
	conn      driver.Conn
	appender  *duckdb.Appender

	mu     sync.Mutex
	closed bool
}

// Open 打开/创建 path 处的 DuckDB，建表并准备好 Appender。
func Open(path string) (*Store, error) {
	connector, err := duckdb.NewConnector(path, nil)
	if err != nil {
		return nil, fmt.Errorf("打开 DuckDB %s：%w", path, err)
	}
	db := sql.OpenDB(connector)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		connector.Close()
		return nil, fmt.Errorf("建表：%w", err)
	}
	conn, err := connector.Connect(context.Background())
	if err != nil {
		db.Close()
		connector.Close()
		return nil, err
	}
	appender, err := duckdb.NewAppenderFromConn(conn, "", "connections")
	if err != nil {
		conn.Close()
		db.Close()
		connector.Close()
		return nil, fmt.Errorf("创建 appender：%w", err)
	}
	return &Store{connector: connector, db: db, conn: conn, appender: appender}, nil
}

// Add 追加一条连接事件（先入 appender 缓冲，Flush/Close 时落地）。
func (s *Store) Add(e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store 已关闭")
	}
	return s.appender.AppendRow(e.TS, e.Process, e.Network, e.Host, int32(e.Port), e.Rule, e.Node, e.Region)
}

// Flush 把 appender 缓冲落地（供定期调用，让数据可查、更耐久）。关闭后为无操作。
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.appender.Flush()
}

// DB 返回可用于只读查询的 *sql.DB（与写入共享同一进程内的 DuckDB，避免跨进程单写锁）。
func (s *Store) DB() *sql.DB { return s.db }

// Close 落地剩余缓冲并释放资源；幂等。
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.appender.Close() // 先 flush 剩余缓冲
	_ = s.conn.Close()
	_ = s.db.Close()
	_ = s.connector.Close()
	return err
}
