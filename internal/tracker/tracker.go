// Package tracker 把连接的目的域名归类为「已知追踪器 + 归属公司」。
//
// 数据来自 DuckDuckGo Tracker Radar 的 domain_map.json（域名 → 归属实体）。其数据许可为
// CC BY-NC-SA 4.0，不随二进制分发；改为运行期由 `inhomo tracker update` 拉到本机缓存后离线比对
// （见 ADR-0011）。数据集未拉取 / 拉取失败 / 断网时，归类器优雅退化为「全部 unknown」，绝不报错阻塞。
//
// 说明：domain_map.json 只给「归属」不给「类别」（advertising/analytics/…），故本包 v0 只做
// 「是否已知追踪器 + 归属公司」；细粒度类别需更重的上游数据源，留作后续。
package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// SourceURL 是 Tracker Radar 合并域名表（域名 → 归属实体）的上游地址。
const SourceURL = "https://raw.githubusercontent.com/duckduckgo/tracker-radar/main/build-data/generated/domain_map.json"

// maxDatasetBytes 是下载上限：防被劫持/异常的镜像撑爆磁盘（当前真实文件约 10MB，留足余量）。
const maxDatasetBytes = 64 << 20 // 64MB

// Classifier 持有「eTLD+1 → 归属公司」映射；只读，可并发查。零值 / nil 视为空（全 unknown）。
type Classifier struct {
	byDomain map[string]string
}

// entry 是 domain_map.json 每个域名的值里我们要的两个字段。
type entry struct {
	DisplayName string `json:"displayName"`
	EntityName  string `json:"entityName"`
}

// Parse 从 domain_map.json 字节构建 Classifier：键（域名）小写化，值取 displayName、空则回落 entityName。
func Parse(data []byte) (*Classifier, error) {
	var raw map[string]entry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析 tracker 数据：%w", err)
	}
	byDomain := make(map[string]string, len(raw))
	for domain, e := range raw {
		owner := e.DisplayName
		if owner == "" {
			owner = e.EntityName
		}
		byDomain[strings.ToLower(domain)] = owner
	}
	return &Classifier{byDomain: byDomain}, nil
}

// Classify 返回 host 的追踪器归属公司及它是否为已知追踪器。按 host 的 eTLD+1 匹配；
// 取不到 eTLD+1 的 host（IP 字面量、单标签、空）→ ("", false)。nil / 空 Classifier 对任意 host 都返回 ("", false)。
func (c *Classifier) Classify(host string) (owner string, known bool) {
	if c == nil || len(c.byDomain) == 0 || host == "" {
		return "", false
	}
	etld1, err := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(host))
	if err != nil {
		return "", false // IP / 单标签 / 无有效后缀 → 未知，绝不报错
	}
	owner, known = c.byDomain[etld1]
	return owner, known
}

// Len 返回已加载的已知追踪器域名条数（nil / 空为 0）。
func (c *Classifier) Len() int {
	if c == nil {
		return 0
	}
	return len(c.byDomain)
}

// Load 从本地缓存文件读并解析 Classifier；文件缺失 → 空 Classifier + 无错（数据集未拉取时全 unknown）。
// 文件存在但解析失败才返回错误。
func Load(path string) (*Classifier, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Classifier{}, nil // 未拉取：空归类器，优雅退化
		}
		return nil, err
	}
	return Parse(data)
}

// CachePath 返回追踪器数据的本地缓存路径 ~/.inhomo/tracker-radar.json（与库/配置同目录）。
func CachePath(home string) string {
	return filepath.Join(home, ".inhomo", "tracker-radar.json")
}

// Fetch 从 SourceURL 下载域名表写入 destPath（先写临时文件再原子改名，避免半截文件）。
func Fetch(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 tracker 数据返回状态 %s", resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destPath), "tracker-radar-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // 成功改名后此 Remove 是无操作
	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, maxDatasetBytes)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, destPath)
}
