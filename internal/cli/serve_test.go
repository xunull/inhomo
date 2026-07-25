package cli

import (
	"testing"
	"time"

	"github.com/xunull/inhomo/internal/store"
	"github.com/xunull/inhomo/internal/tracker"
)

func TestParseDur(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},
		{"24h", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"90m", 90 * time.Minute, false},
		{"bad", 0, true},
		{"xd", 0, true},
		{"-5d", 0, true}, // 负天数应报错，而非静默全量
	}
	for _, c := range cases {
		got, err := parseDur(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseDur(%q) err=%v，wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.want {
			t.Errorf("parseDur(%q)=%v，期望 %v", c.in, got, c.want)
		}
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8464", true},
		{"localhost:8464", true},
		{"[::1]:8464", true},
		{"0.0.0.0:8464", false},
		{"192.168.1.10:8464", false},
		{"127.0.0.1", true}, // 无端口也判回环
	}
	for _, c := range cases {
		if got := isLoopbackAddr(c.addr); got != c.want {
			t.Errorf("isLoopbackAddr(%q)=%v，期望 %v", c.addr, got, c.want)
		}
	}
}

// TestIsMutedProcess 钉住「用户指定目的地的进程」的判定（见 CONTEXT 术语、ADR-0014）。
// 关键不变量：内置默认名单只列浏览器名、走子串匹配，因此 `Google Chrome Helper` 会命中，
// 而各种**非浏览器**的 Helper（小程序容器、AI 客户端）不会被误伤——它们是信号而非噪音。
func TestIsMutedProcess(t *testing.T) {
	muted := splitList(defaultMuteProcesses)

	shouldMute := []string{
		"Google Chrome",
		"Google Chrome Helper", // 真正发起连接的进程名，必须命中
		"Safari",
		"firefox", // 大小写不敏感
	}
	for _, p := range shouldMute {
		if !isMutedProcess(p, muted) {
			t.Errorf("%q 属于用户指定目的地的进程，应被折叠", p)
		}
	}

	// 这几个是本视图最值钱的信号来源，绝不能因为名字里有 Helper/Browser 就被折叠掉。
	shouldNotMute := []string{
		"MiniProgramEx Helper",   // 小程序容器：用户点开它，但目的地由 App 自己定
		"SomeApp Browser Helper", // 关键用例：名字里带 Browser，却不是主流浏览器
		"AIClient Helper",
		"WeatherWidget",
		"nsurlsessiond", // 系统守护进程（每台 Mac 都有，非特定用户）
		"某音乐播放器",        // 非 ASCII 名字同样不该被误伤
	}
	for _, p := range shouldNotMute {
		if isMutedProcess(p, muted) {
			t.Errorf("%q 的目的地由 App 自己指定，不应被折叠", p)
		}
	}

	// 名单为空 → 什么都不折叠（用户显式清空即「别替我藏任何东西」）。
	if isMutedProcess("Google Chrome", nil) {
		t.Error("名单为空时不应折叠任何进程")
	}
}

// TestSplitList 覆盖逗号名单解析：去空白、丢空项、空串得空名单。
func TestSplitList(t *testing.T) {
	got := splitList(" a , ,b,, c ")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("应解析出 %v，得 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 项应为 %q，得 %q", i, want[i], got[i])
		}
	}
	if len(splitList("")) != 0 || len(splitList("  ,  ")) != 0 {
		t.Error("空串 / 全空白应得空名单")
	}
}

// TestGroupNewChannels 覆盖按 App 归组：计数、追踪器归属标注、
// 以及「被折叠的 App 沉底但保留」这一取舍（只标记不删除）。
func TestGroupNewChannels(t *testing.T) {
	cl, err := tracker.Parse([]byte(`{"google-analytics.com": {"displayName": "Google"}}`))
	if err != nil {
		t.Fatal(err)
	}
	muted := splitList(defaultMuteProcesses)

	// 入参已按 (process, 首次时刻倒序) 排好（NewChannels 的约定）。
	chs := []store.NewChannel{
		{Process: "Google Chrome Helper", Host: "a.com"}, // 被折叠的 App，但通道数最多
		{Process: "Google Chrome Helper", Host: "b.com"},
		{Process: "Google Chrome Helper", Host: "c.com"},
		{Process: "SomeWidget", Host: "www.google-analytics.com"}, // 命中追踪器
		{Process: "SomeWidget", Host: "x.com"},
		{Process: "SomeDaemon", Host: "y.com"},
	}
	groups := groupNewChannels(chs, muted, cl)

	if len(groups) != 3 {
		t.Fatalf("应归出 3 个 App 组，得 %d：%+v", len(groups), groups)
	}
	// 未折叠的排前面，组内按新增数降序：SomeWidget(2) → SomeDaemon(1) → Chrome(3，折叠沉底)。
	if groups[0].Process != "SomeWidget" || groups[1].Process != "SomeDaemon" {
		t.Errorf("未折叠的 App 应按新增数降序排在前面，得 %q / %q", groups[0].Process, groups[1].Process)
	}
	last := groups[len(groups)-1]
	if last.Process != "Google Chrome Helper" || !last.Muted {
		t.Errorf("被折叠的 App 应沉底并标 muted，得 %+v", last)
	}
	if last.Count != 3 || len(last.Channels) != 3 {
		t.Errorf("被折叠的 App 仍须**保留**其全部通道（只标记不删除），得 count=%d len=%d", last.Count, len(last.Channels))
	}
	if groups[0].Count != 2 {
		t.Errorf("SomeWidget 应有 2 条新增通道，得 %d", groups[0].Count)
	}
	if got := groups[0].Channels[0].Tracker; got != "Google" {
		t.Errorf("www.google-analytics.com 应归属 Google，得 %q", got)
	}
	if got := groups[0].Channels[1].Tracker; got != "" {
		t.Errorf("未命中追踪器的通道归属应为空，得 %q", got)
	}

	// 空输入 → 空组（非 nil，JSON 为 []）。
	if g := groupNewChannels(nil, muted, cl); g == nil || len(g) != 0 {
		t.Errorf("空输入应得空切片，得 %+v", g)
	}
	// nil classifier 不 panic（未拉取追踪器数据的情形）。
	if g := groupNewChannels(chs, muted, nil); len(g) != 3 {
		t.Errorf("nil classifier 时仍应正常归组，得 %+v", g)
	}
}
