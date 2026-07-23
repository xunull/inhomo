package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// vergeConfigYAML 是一份 Clash Verge Rev 式运行时 mihomo 配置 fixture：unix socket + TCP + secret 三者齐备。
// secret 用假值，测试断言它绝不出现在任何面向用户的说明里。
const vergeConfigYAML = `mixed-port: 7897
external-controller: 127.0.0.1:9097
external-controller-unix: /tmp/verge/verge-mihomo.sock
secret: fake-secret-xyz
`

// TestCandidatesFrom 是纯解析接缝：一份 mihomo 配置 → 有序候选（unix 优先、其次 TCP，均带该配置的 secret）。
func TestCandidatesFrom(t *testing.T) {
	t.Run("unix+TCP+secret → 两候选，unix 优先", func(t *testing.T) {
		got := candidatesFrom([]byte(vergeConfigYAML), "Clash Verge Rev")
		want := []discovered{
			{controller: "unix:///tmp/verge/verge-mihomo.sock", secret: "fake-secret-xyz", source: "Clash Verge Rev"},
			{controller: "127.0.0.1:9097", secret: "fake-secret-xyz", source: "Clash Verge Rev"},
		}
		if len(got) != len(want) {
			t.Fatalf("候选数=%d，期望 %d：%+v", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("候选[%d]=%+v，期望 %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("仅 TCP", func(t *testing.T) {
		got := candidatesFrom([]byte("external-controller: 127.0.0.1:9090\n"), "x")
		if len(got) != 1 || got[0].controller != "127.0.0.1:9090" {
			t.Errorf("仅 TCP 应得单个 TCP 候选，得 %+v", got)
		}
	})

	t.Run("仅 unix", func(t *testing.T) {
		got := candidatesFrom([]byte("external-controller-unix: /tmp/x.sock\n"), "x")
		if len(got) != 1 || got[0].controller != "unix:///tmp/x.sock" {
			t.Errorf("仅 unix 应得单个 unix 候选，得 %+v", got)
		}
	})

	t.Run("两个 controller 键都空 → nil", func(t *testing.T) {
		if got := candidatesFrom([]byte("mixed-port: 7897\n"), "x"); got != nil {
			t.Errorf("无 controller 键应得 nil，得 %+v", got)
		}
	})

	t.Run("非法 YAML → nil", func(t *testing.T) {
		if got := candidatesFrom([]byte("external-controller: [未闭合\n"), "x"); got != nil {
			t.Errorf("非法 YAML 应得 nil，得 %+v", got)
		}
	})
}

// mihomoNativeConfigYAML 是一份「被定制过」的裸 mihomo 配置 fixture：非默认端口 9091 + secret、无 unix socket。
// 默认 9090 无 secret 的裸 mihomo 由 T39 的兜底直接覆盖，不必读配置；本 fixture 正对应必须读配置才连得上的那类。
const mihomoNativeConfigYAML = `mixed-port: 7890
mode: rule
log-level: info
external-controller: 127.0.0.1:9091
secret: bare-secret-abc
`

// TestCandidatesFrom_bareMihomo 补裸 mihomo 式配置的解析单测（T40 验收）：非默认端口 + secret → 单个 TCP 候选带 secret。
// 复用 T39 的 candidatesFrom，不新造解析器。
func TestCandidatesFrom_bareMihomo(t *testing.T) {
	got := candidatesFrom([]byte(mihomoNativeConfigYAML), "mihomo")
	want := discovered{controller: "127.0.0.1:9091", secret: "bare-secret-abc", source: "mihomo"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("裸 mihomo 配置应得单个 TCP 候选 %+v，得 %+v", want, got)
	}
}

// TestMihomoNativeConfigSources 断言裸 mihomo 默认配置路径 = ~/.config/mihomo/config.yaml、来源标签 mihomo。
func TestMihomoNativeConfigSources(t *testing.T) {
	got := mihomoNativeConfigSources("/home/u")
	if len(got) != 1 {
		t.Fatalf("应给一处候选路径，得 %d", len(got))
	}
	if want := filepath.Join("/home", "u", ".config", "mihomo", "config.yaml"); got[0].path != want {
		t.Errorf("路径=%q，期望 %q", got[0].path, want)
	}
	if got[0].source != "mihomo" {
		t.Errorf("来源=%q，期望 mihomo", got[0].source)
	}
}

// TestDiscoverySources 钉死固定来源顺序：Clash Verge Rev（v0 主场景）在前、裸 mihomo 在后。
func TestDiscoverySources(t *testing.T) {
	got := discoverySources("/home/u")
	if len(got) < 2 {
		t.Fatalf("应含 Verge + 裸 mihomo 两源，得 %d：%+v", len(got), got)
	}
	if got[0].source != "Clash Verge Rev" {
		t.Errorf("首源=%q，期望 Clash Verge Rev（优先）", got[0].source)
	}
	if got[len(got)-1].source != "mihomo" {
		t.Errorf("末源=%q，期望 mihomo（裸 mihomo 在后）", got[len(got)-1].source)
	}
}

// TestGatherCandidates 覆盖多来源汇聚：可读的解析出候选，缺失/不可读的静默跳过（守护进程不因某客户端未装而报错）。
func TestGatherCandidates(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/verge/config.yaml":
			return []byte(vergeConfigYAML), nil
		default:
			return nil, os.ErrNotExist
		}
	}
	sources := []configSource{
		{path: "/missing/config.yaml", source: "缺席客户端"},
		{path: "/verge/config.yaml", source: "Clash Verge Rev"},
	}
	got := gatherCandidates(sources, readFile)
	if len(got) != 2 {
		t.Fatalf("应从可读来源得 2 候选、跳过缺失来源，得 %d：%+v", len(got), got)
	}
	if got[0].source != "Clash Verge Rev" {
		t.Errorf("候选来源=%q，期望 Clash Verge Rev", got[0].source)
	}

	t.Run("全部不可读 → nil", func(t *testing.T) {
		got := gatherCandidates(sources, func(string) ([]byte, error) { return nil, errors.New("boom") })
		if got != nil {
			t.Errorf("全部不可读应得 nil，得 %+v", got)
		}
	})
}

// TestFirstLive 覆盖探活挑选：按序取第一个活的；顺序有意义；都不活/空表 → 未找到；探针拿到候选的 controller+secret。
func TestFirstLive(t *testing.T) {
	cands := []discovered{
		{controller: "unix:///tmp/a.sock", secret: "sa", source: "A"},
		{controller: "127.0.0.1:9097", secret: "sb", source: "A"},
	}

	t.Run("第二个才活 → 返回第二个", func(t *testing.T) {
		got, ok := firstLive(cands, func(controller, _ string) bool { return controller == "127.0.0.1:9097" })
		if !ok || got.controller != "127.0.0.1:9097" {
			t.Errorf("应挑中第二个活候选，得 (%+v,%v)", got, ok)
		}
	})

	t.Run("第一个就活 → 返回第一个（顺序优先）", func(t *testing.T) {
		got, ok := firstLive(cands, func(string, string) bool { return true })
		if !ok || got.controller != "unix:///tmp/a.sock" {
			t.Errorf("都活时应取顺序第一，得 (%+v,%v)", got, ok)
		}
	})

	t.Run("都不活 → 未找到", func(t *testing.T) {
		if _, ok := firstLive(cands, func(string, string) bool { return false }); ok {
			t.Error("都不活应返回未找到")
		}
	})

	t.Run("空表 → 未找到", func(t *testing.T) {
		if _, ok := firstLive(nil, func(string, string) bool { return true }); ok {
			t.Error("空候选表应返回未找到")
		}
	})

	t.Run("探针拿到候选的 controller+secret", func(t *testing.T) {
		var gotCtrl, gotSecret string
		firstLive([]discovered{{controller: "c1", secret: "s1"}}, func(controller, secret string) bool {
			gotCtrl, gotSecret = controller, secret
			return true
		})
		if gotCtrl != "c1" || gotSecret != "s1" {
			t.Errorf("探针收到 (%q,%q)，期望 (c1,s1)", gotCtrl, gotSecret)
		}
	})
}

// TestApplyDiscovery 覆盖「只填空、显式恒赢」的并入规则与透明说明；secret 绝不出现在说明里。
func TestApplyDiscovery(t *testing.T) {
	disc := discovered{controller: "unix:///tmp/verge.sock", secret: "disc-secret", source: "Clash Verge Rev"}

	t.Run("发现成功、secret 未显式 → 用发现的 controller+secret", func(t *testing.T) {
		ctrl, secret, msg := applyDiscovery("127.0.0.1:9090", "", false, disc, true)
		if ctrl != "unix:///tmp/verge.sock" || secret != "disc-secret" {
			t.Errorf("得 (%q,%q)，期望 (unix:///tmp/verge.sock, disc-secret)", ctrl, secret)
		}
		if !strings.Contains(msg, "Clash Verge Rev") || !strings.Contains(msg, "unix:///tmp/verge.sock") {
			t.Errorf("说明应含来源与 controller，得 %q", msg)
		}
		if strings.Contains(msg, "disc-secret") {
			t.Errorf("说明泄露了 secret：%q", msg)
		}
	})

	t.Run("发现成功、secret 已显式 → 保留显式 secret、只用发现的 controller", func(t *testing.T) {
		ctrl, secret, msg := applyDiscovery("127.0.0.1:9090", "my-explicit", true, disc, true)
		if ctrl != "unix:///tmp/verge.sock" || secret != "my-explicit" {
			t.Errorf("得 (%q,%q)，期望 (unix:///tmp/verge.sock, my-explicit)", ctrl, secret)
		}
		if strings.Contains(msg, "my-explicit") || strings.Contains(msg, "disc-secret") {
			t.Errorf("说明泄露了 secret：%q", msg)
		}
	})

	t.Run("未发现 → 回退传入的 controller/secret，说明为回退提示", func(t *testing.T) {
		ctrl, secret, msg := applyDiscovery("127.0.0.1:9090", "keep", false, discovered{}, false)
		if ctrl != "127.0.0.1:9090" || secret != "keep" {
			t.Errorf("未发现应保持回退值，得 (%q,%q)", ctrl, secret)
		}
		if !strings.Contains(msg, "127.0.0.1:9090") || !strings.Contains(msg, "回退") {
			t.Errorf("说明应提示回退默认，得 %q", msg)
		}
	})
}

// TestHasExplicitSource 覆盖「显式来源」判定：改过的 flag / INHOMO_ 环境变量 / 配置文件有该键 任一即显式；
// 三者皆无 → 非显式（这正是触发自动发现的条件）。经 bindConfig 建真 viper，验 InConfig 分支。
func TestHasExplicitSource(t *testing.T) {
	// newBoundCmd 造一个带 controller flag、经 bindConfig 建好配置的命令（HOME 指向可选含 config 的临时目录）。
	newBoundCmd := func(t *testing.T, configBody string) *cobra.Command {
		t.Helper()
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".inhomo"), 0o755); err != nil {
			t.Fatal(err)
		}
		if configBody != "" {
			if err := os.WriteFile(filepath.Join(home, ".inhomo", "config.yaml"), []byte(configBody), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Setenv("HOME", home)
		cmd := &cobra.Command{}
		cmd.Flags().String(flagController, "127.0.0.1:9090", "")
		cmd.SetContext(context.Background())
		if err := bindConfig(cmd); err != nil {
			t.Fatalf("bindConfig：%v", err)
		}
		return cmd
	}

	t.Run("三者皆无 → 非显式", func(t *testing.T) {
		cmd := newBoundCmd(t, "")
		if hasExplicitSource(cmd, cfgOf(cmd), flagController) {
			t.Error("无 flag/env/config 应判非显式")
		}
	})

	t.Run("改过的 flag → 显式", func(t *testing.T) {
		cmd := newBoundCmd(t, "")
		_ = cmd.Flags().Set(flagController, "unix:///x.sock")
		if !hasExplicitSource(cmd, cfgOf(cmd), flagController) {
			t.Error("改过的 flag 应判显式")
		}
	})

	t.Run("INHOMO_ 环境变量 → 显式", func(t *testing.T) {
		t.Setenv("INHOMO_CONTROLLER", "envval")
		cmd := newBoundCmd(t, "")
		if !hasExplicitSource(cmd, cfgOf(cmd), flagController) {
			t.Error("设了 INHOMO_CONTROLLER 应判显式")
		}
	})

	t.Run("配置文件有该键 → 显式", func(t *testing.T) {
		cmd := newBoundCmd(t, "controller: unix:///cfg.sock\n")
		if !hasExplicitSource(cmd, cfgOf(cmd), flagController) {
			t.Error("config 里有 controller 键应判显式")
		}
	})
}

// TestVergeConfigSources 断言当前 OS 下 Verge 配置候选路径落在其 app-data 目录、文件名为 config.yaml。
func TestVergeConfigSources(t *testing.T) {
	got := vergeConfigSources("/home/u")
	if len(got) == 0 {
		t.Fatal("应至少给一处候选路径")
	}
	p := got[0].path
	if filepath.Base(p) != "config.yaml" {
		t.Errorf("配置文件名=%q，期望 config.yaml", filepath.Base(p))
	}
	if !strings.Contains(p, "io.github.clash-verge-rev.clash-verge-rev") {
		t.Errorf("路径应含 Verge app-id，得 %q", p)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(p, filepath.Join("Library", "Application Support")) {
		t.Errorf("macOS 路径应在 Library/Application Support 下，得 %q", p)
	}
	if got[0].source == "" {
		t.Error("来源标签不应为空")
	}
}
