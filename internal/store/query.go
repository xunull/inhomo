package store

import (
	"database/sql"
	"time"
)

// Summary 是 /api/summary 的返回：总量与几个关键分布。
type Summary struct {
	Total     int64      `json:"total"`     // 总连接事件数
	Hosts     int64      `json:"hosts"`     // 去重目的 host 数
	Processes int64      `json:"processes"` // 去重进程数（不含空）
	Nodes     int64      `json:"nodes"`     // 去重出境节点数
	Direct    int64      `json:"direct"`    // 直连（node=DIRECT）
	Proxied   int64      `json:"proxied"`   // 经真实出境节点（排除 DIRECT / REJECT* / 空，口径同 detect.isEgressNode）
	HTTP      int64      `json:"http"`      // 目的端口 80
	HTTPS     int64      `json:"https"`     // 目的端口 443
	Earliest  *time.Time `json:"earliest"`  // 最早一条（空库为 null）
	Latest    *time.Time `json:"latest"`    // 最晚一条
}

// Summary 一次查出总量与分布（空库返回全 0、时间为 null，不报错）。
func (s *Store) Summary() (Summary, error) {
	const q = `SELECT
		count(*),
		count(DISTINCT host),
		count(DISTINCT nullif(process,'')),
		count(DISTINCT node),
		count(*) FILTER (WHERE node = 'DIRECT'),
		count(*) FILTER (WHERE node <> '' AND node <> 'DIRECT' AND node NOT LIKE 'REJECT%'),
		count(*) FILTER (WHERE port = 80),
		count(*) FILTER (WHERE port = 443),
		min(ts), max(ts)
		FROM connections`

	var sm Summary
	var earliest, latest sql.NullTime
	err := s.DB().QueryRow(q).Scan(
		&sm.Total, &sm.Hosts, &sm.Processes, &sm.Nodes,
		&sm.Direct, &sm.Proxied, &sm.HTTP, &sm.HTTPS,
		&earliest, &latest,
	)
	if err != nil {
		return Summary{}, err
	}
	if earliest.Valid {
		sm.Earliest = &earliest.Time
	}
	if latest.Valid {
		sm.Latest = &latest.Time
	}
	return sm, nil
}
