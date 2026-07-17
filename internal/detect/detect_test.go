package detect

import "testing"

// 下列日志样本按 mihomo `tunnel.go` 的 logMetadata 实际输出格式构造（格式忠实于源码，
// 非真机采集）；接入真实 mihomo 后可补充实采样本，进一步校验解析健壮性。

// ports80 是测试用的默认 HTTP 端口集。
var ports80 = map[int]bool{80: true}

// TestParseClassify_core 覆盖核心三态：端口 80 经代理→泄露；443→非泄露；DIRECT→非泄露。
func TestParseClassify_core(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		isLeak bool
		host   string
		port   int
		node   string
	}{
		{
			name:   "端口80经代理→泄露",
			line:   "[TCP] 192.168.1.5:52341 --> plain.example.com:80 match DomainKeyword(example) using 🇺🇸 US-02",
			isLeak: true, host: "plain.example.com", port: 80, node: "🇺🇸 US-02",
		},
		{
			name:   "端口443→非泄露",
			line:   "[TCP] 192.168.1.5:52799 --> secure.example.com:443 match DomainSuffix(example.com) using 🇭🇰 HK-01",
			isLeak: false,
		},
		{
			name:   "DIRECT→非泄露",
			line:   "[TCP] 192.168.1.5:53000 --> intranet.local:80 using DIRECT",
			isLeak: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, ok := Parse(c.line)
			if !ok {
				t.Fatalf("Parse 未能解析连接日志行：%q", c.line)
			}
			leak, isLeak := Classify(ev, ports80)
			if isLeak != c.isLeak {
				t.Fatalf("isLeak=%v，期望 %v", isLeak, c.isLeak)
			}
			if !c.isLeak {
				return
			}
			if leak.Host != c.host || leak.Port != c.port || leak.Node != c.node {
				t.Fatalf("泄露事件字段不符：得到 host=%q port=%d node=%q", leak.Host, leak.Port, leak.Node)
			}
		})
	}
}

// TestClassify_egressAndPorts 覆盖出境节点判定与端口/网络过滤：
// GLOBAL 算出境；REJECT 系列不算；UDP 即便端口 80 也不算（明文 HTTP 是 TCP）。
func TestClassify_egressAndPorts(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		isLeak bool
		node   string
	}{
		{"GLOBAL→泄露(节点记GLOBAL)", "[TCP] 10.0.0.1:1 --> a.com:80 using GLOBAL", true, "GLOBAL"},
		{"REJECT→非泄露", "[TCP] 10.0.0.1:2 --> ad.com:80 match Domain(ad.com) using REJECT", false, ""},
		{"REJECT-DROP→非泄露", "[TCP] 10.0.0.1:2 --> ad.com:80 match Domain(ad.com) using REJECT-DROP", false, ""},
		{"UDP端口80→非泄露(非TCP)", "[UDP] 10.0.0.1:3 --> q.com:80 using 🇯🇵 JP-01", false, ""},
		{"specialProxy无match→泄露", "[TCP] 10.0.0.1:4 --> plain.io:80 using 🇸🇬 SG-01", true, "🇸🇬 SG-01"},
		{"无规则命中→泄露", "[TCP] 10.0.0.1:5 --> x.io:80 doesn't match any rule using 🇺🇸 US-9", true, "🇺🇸 US-9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, ok := Parse(c.line)
			if !ok {
				t.Fatalf("Parse 失败：%q", c.line)
			}
			leak, isLeak := Classify(ev, ports80)
			if isLeak != c.isLeak {
				t.Fatalf("isLeak=%v，期望 %v", isLeak, c.isLeak)
			}
			if c.isLeak && leak.Node != c.node {
				t.Fatalf("node=%q，期望 %q", leak.Node, c.node)
			}
		})
	}
}

// TestParse_fields 覆盖 host:port 切分（含 IPv6）、rule 提取，以及无法解析的行。
func TestParse_fields(t *testing.T) {
	// IPv6 目的：按最后一个 ':' 切端口。
	ev, ok := Parse("[TCP] 10.0.0.1:6 --> [2001:db8::1]:80 match GeoIP(us) using 🇺🇸 US-1")
	if !ok {
		t.Fatal("IPv6 行应解析成功")
	}
	if ev.Host != "[2001:db8::1]" || ev.Port != 80 {
		t.Fatalf("IPv6 切分错：host=%q port=%d", ev.Host, ev.Port)
	}
	if ev.Rule != "GeoIP(us)" {
		t.Fatalf("rule 提取错：%q", ev.Rule)
	}

	// 无规则命中：rule 保留整句。
	if ev2, _ := Parse("[TCP] 10.0.0.1:7 --> x.io:80 doesn't match any rule using N1"); ev2.Rule != "doesn't match any rule" {
		t.Fatalf("无规则串 rule=%q", ev2.Rule)
	}

	// specialProxy：无 rule。
	if ev3, _ := Parse("[TCP] 10.0.0.1:8 --> y.io:80 using N2"); ev3.Rule != "" {
		t.Fatalf("specialProxy rule 应为空，得 %q", ev3.Rule)
	}

	// 无法解析：非连接日志 / 缺字段 / 端口非法。
	for _, bad := range []string{
		"[Sniffer] Sniff TCP [1.2.3.4:1] with sniff host example.com",
		"time=xxx level=info msg=started",
		"[TCP] 1.2.3.4:1 --> host-without-port using N",
		"[TCP] 1.2.3.4:1 --> h:notaport using N",
		"[TCP] 1.2.3.4:1 --> h:80", // 缺 using
		"",
	} {
		if _, ok := Parse(bad); ok {
			t.Fatalf("应判为无法解析：%q", bad)
		}
	}
}

// TestRegion 覆盖地区标签解析：国旗 emoji → ISO 两字母码；中文关键词；都不中 → unknown。
func TestRegion(t *testing.T) {
	cases := []struct{ node, want string }{
		{"🇺🇸 US-02", "US"},
		{"🇭🇰 HK-01", "HK"},
		{"香港01", "HK"},
		{"日本 IEPL 03", "JP"},
		{"Node-3", "unknown"},
		{"中转-A", "unknown"},
		{"GLOBAL", "unknown"},
	}
	for _, c := range cases {
		if got := Region(c.node); got != c.want {
			t.Errorf("Region(%q)=%q，期望 %q", c.node, got, c.want)
		}
	}
}

// TestParseClassify_realChainFormat 用真机采集的链路格式「分组名[真实节点|倍率]」验证判定。
func TestParseClassify_realChainFormat(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		isLeak bool
		node   string // 期望的出境节点（末端）
		region string
	}{
		{
			name:   "分组[DIRECT] 80端口 → 非泄露（曾误报的真机 bug）",
			line:   "[TCP] 198.18.0.1:1(app) --> plain.cn:80 match GeoIP(CN) using 🎯 全球直连[DIRECT]",
			isLeak: false,
		},
		{
			name:   "分组[节点|倍率] 80端口 → 泄露，取末端节点(名字含|倍率原样保留)+地区",
			line:   "[TCP] 198.18.0.1:2(app) --> plain.us:80 match DomainKeyword(x) using 🚀 节点选择[🇺🇸美国HY2-07|1.0X]",
			isLeak: true, node: "🇺🇸美国HY2-07|1.0X", region: "US",
		},
		{
			name:   "真机 443 经代理 → 非泄露",
			line:   "[TCP] 198.18.0.1:64245(gh) --> api.github.com:443 match DomainKeyword(github) using 🚀 节点选择[🇺🇸美国HY2-07|1.0X]",
			isLeak: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cl, ok := Parse(c.line)
			if !ok {
				t.Fatalf("Parse 失败：%q", c.line)
			}
			leak, isLeak := Classify(cl, ports80)
			if isLeak != c.isLeak {
				t.Fatalf("isLeak=%v，期望 %v（解析出的 node=%q）", isLeak, c.isLeak, cl.Node)
			}
			if c.isLeak && (leak.Node != c.node || leak.Region != c.region) {
				t.Fatalf("出境节点/地区不符：node=%q region=%q，期望 %q/%q", leak.Node, leak.Region, c.node, c.region)
			}
		})
	}
}

// TestEffectiveNode 直接覆盖链路末端节点提取。
func TestEffectiveNode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"🎯 全球直连[DIRECT]", "DIRECT"},
		{"🚀 节点选择[🇺🇸美国HY2-07|1.0X]", "🇺🇸美国HY2-07|1.0X"}, // 保留节点名(含 |倍率)
		{"A[B[REJECT]]", "REJECT"},
		{"🇺🇸 US-02", "🇺🇸 US-02"}, // 无括号，原样
		{"DIRECT", "DIRECT"},
		{"分组[🇭🇰香港|IEPL|01]", "🇭🇰香港|IEPL|01"}, // 名字自带 | 不被截断
	}
	for _, c := range cases {
		if got := effectiveNode(c.in); got != c.want {
			t.Errorf("effectiveNode(%q)=%q，期望 %q", c.in, got, c.want)
		}
	}
}
