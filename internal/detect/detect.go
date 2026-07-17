// Package detect 是 mihomo 日志行 → 明文 HTTP 泄露事件的纯判定管线（Seam 1）。
// Parse 与 Classify 均为纯函数（无 I/O、无 time.Now），便于表驱动测试。
package detect

import (
	"strconv"
	"strings"
)

// ConnLog 是从一行 mihomo 连接日志解析出的结构化中间结果（与 LeakEvent 配对：前者原始、后者判定后）。
type ConnLog struct {
	Network string // TCP / UDP
	Host    string // 目的 host（域名或 IP；IPv6 形如 [::1]）
	Port    int    // 目的端口
	Rule    string // 命中规则文本，如 "DomainKeyword(example)"；specialProxy / 无规则时可能为空或说明串
	Node    string // 出境节点：已从「分组名[真实节点|倍率]」解析出的末端节点，如 "🇺🇸美国HY2-07"、"DIRECT"
}

// LeakEvent 是一次「明文 HTTP 泄露事件」。TS 由消费方在收到时戳（保持 Parse/Classify 纯粹）。
type LeakEvent struct {
	Host   string
	Port   int
	Node   string
	Region string
	Rule   string
}

// Parse 把一行 mihomo 连接日志解析成 ConnLog。仅接受形如
//
//	[TCP] <src> --> <host:port> [match <rule> | doesn't match any rule] using <node>
//
// 的连接日志行；其它日志（Rule/Sniffer/DNS 等）或格式异常行返回 ok=false，交由上层计数跳过。
func Parse(line string) (ConnLog, bool) {
	line = strings.TrimSpace(line)

	// 1. 网络类型：[TCP] / [UDP]
	if !strings.HasPrefix(line, "[") {
		return ConnLog{}, false
	}
	rb := strings.IndexByte(line, ']')
	if rb < 0 {
		return ConnLog{}, false
	}
	network := line[1:rb]
	if network != "TCP" && network != "UDP" {
		return ConnLog{}, false
	}
	rest := strings.TrimSpace(line[rb+1:])

	// 2. 跳过 "src --> "，取箭头之后的 tail（源地址本期不消费）
	_, tail, found := strings.Cut(rest, " --> ")
	if !found {
		return ConnLog{}, false
	}

	// 3. node = 最后一个 " using " 之后（节点名可能含空格/emoji，故取末段）
	const usingSep = " using "
	ui := strings.LastIndex(tail, usingSep)
	if ui < 0 {
		return ConnLog{}, false
	}
	node := strings.TrimSpace(tail[ui+len(usingSep):])
	if node == "" {
		return ConnLog{}, false
	}
	node = effectiveNode(node) // 从「分组名[真实节点|倍率]」取末端出境节点
	if node == "" {
		return ConnLog{}, false
	}

	// 4. dst = tail 里 --> 之后的第一个 token（host:port 无空格）；其余是 rule 说明
	//    （specialProxy 无 rule 时 Cut 未命中，dst=middle、ruleText=""）。
	middle := strings.TrimSpace(tail[:ui])
	dst, ruleText, _ := strings.Cut(middle, " ")
	rule := strings.TrimPrefix(strings.TrimSpace(ruleText), "match ")

	// 5. host:port（按最后一个 ':' 切，兼容 IPv6 [::1]:80 与域名）
	host, port, ok := splitHostPort(dst)
	if !ok {
		return ConnLog{}, false
	}

	return ConnLog{Network: network, Host: host, Port: port, Rule: rule, Node: node}, true
}

// splitHostPort 按最后一个 ':' 切分，兼容域名、IPv4 与带方括号的 IPv6。
func splitHostPort(dst string) (host string, port int, ok bool) {
	ci := strings.LastIndexByte(dst, ':')
	if ci <= 0 || ci == len(dst)-1 {
		return "", 0, false
	}
	p, ok := ParsePort(dst[ci+1:])
	if !ok {
		return "", 0, false
	}
	return dst[:ci], p, true
}

// ParsePort 解析端口字符串并校验取值域 (0, 65535]。供日志解析与配置端口集共用。
func ParsePort(s string) (int, bool) {
	p, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || p <= 0 || p > 65535 {
		return 0, false
	}
	return p, true
}

// effectiveNode 从 mihomo 的链路串里取出真正的末端出境节点：
//
//	"分组名[真实节点]" → "真实节点"；嵌套 "A[B[C]]" → "C"；无括号则原样返回（去空白）。
//
// '['/']' 均为单字节 ASCII，按字节定位对 UTF-8 安全。刻意**不**去除节点名里的 "|倍率"——
// 真实订阅的节点名常自带 '|'（如 "🇭🇰香港|IEPL|01"），截断会误伤，故原样保留节点名。
func effectiveNode(raw string) string {
	s := strings.TrimSpace(raw)
	for {
		open := strings.LastIndexByte(s, '[')
		if open < 0 {
			break
		}
		end := strings.IndexByte(s[open:], ']')
		if end < 0 {
			break
		}
		s = s[open+1 : open+end]
	}
	return strings.TrimSpace(s)
}

// Classify 判定一个 ConnLog 是否为明文 HTTP 泄露事件：
// 明文 HTTP = TCP 且目的端口 ∈ httpPorts（明文 HTTP 即 TCP，故排除 UDP）；
// 泄露 = 明文 HTTP 且经真实出境节点（非 DIRECT/REJECT）。
func Classify(c ConnLog, httpPorts map[int]bool) (LeakEvent, bool) {
	if c.Network != "TCP" || !httpPorts[c.Port] || !isEgressNode(c.Node) {
		return LeakEvent{}, false
	}
	return LeakEvent{
		Host:   c.Host,
		Port:   c.Port,
		Node:   c.Node,
		Region: Region(c.Node),
		Rule:   c.Rule,
	}, true
}

// isEgressNode 判断该 node 是否为「真实出境节点」——排除 DIRECT 与 REJECT 系列（不构成中转泄露）；
// 具名节点与 GLOBAL 均视为出境节点。
func isEgressNode(node string) bool {
	switch {
	case node == "", node == "DIRECT":
		return false
	case strings.HasPrefix(node, "REJECT"): // REJECT / REJECT-DROP
		return false
	default:
		return true
	}
}

// Region 从出境节点名尽力解析地区标签：优先国旗 emoji（→ ISO 3166-1 alpha-2），
// 其次保守的中/英文国家地区关键词；都命中不了记 "unknown"（ADR-0001：仅作标签、不作硬筛）。
func Region(node string) string {
	if code, ok := flagToISO(node); ok {
		return code
	}
	if code, ok := keywordToISO(node); ok {
		return code
	}
	return "unknown"
}

// flagToISO 在字符串里找第一对「区域指示符」（两个 U+1F1E6..U+1F1FF），解码成两字母国家码。
func flagToISO(s string) (string, bool) {
	const base = 0x1F1E6 // 区域指示符 'A'
	rs := []rune(s)
	for i := 0; i+1 < len(rs); i++ {
		a, b := rs[i], rs[i+1]
		if a >= 0x1F1E6 && a <= 0x1F1FF && b >= 0x1F1E6 && b <= 0x1F1FF {
			return string([]byte{byte('A' + (a - base)), byte('A' + (b - base))}), true
		}
	}
	return "", false
}

// keywords 是保守的关键词表（按序匹配，先中者先得）。刻意不放裸的两字母码（如 "US"），
// 因为它们作为子串太易误命中普通节点名；带国旗的节点走 flagToISO，无国旗的宁可记 unknown。
var keywords = []struct{ sub, code string }{
	{"香港", "HK"}, {"Hong Kong", "HK"},
	{"台湾", "TW"}, {"Taiwan", "TW"},
	{"日本", "JP"}, {"Japan", "JP"},
	{"新加坡", "SG"}, {"Singapore", "SG"},
	{"美国", "US"}, {"United States", "US"},
	{"韩国", "KR"}, {"Korea", "KR"},
	{"英国", "GB"}, {"United Kingdom", "GB"},
	{"德国", "DE"}, {"Germany", "DE"},
}

func keywordToISO(s string) (string, bool) {
	for _, kw := range keywords {
		if strings.Contains(s, kw.sub) {
			return kw.code, true
		}
	}
	return "", false
}
