package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 注入的版本号要能从 `inhomo version` 与 `inhomo --version` 两处读出来（发布时 CI 经 ldflags 注入的就是它）。
func TestVersion_printsInjected(t *testing.T) {
	const want = "v0.9.9-test"

	t.Run("version 子命令", func(t *testing.T) {
		var out bytes.Buffer
		root := newRootCmd(want)
		root.SetOut(&out)
		root.SetArgs([]string{"version"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), want) {
			t.Errorf("version 输出 %q 不含注入版本 %q", out.String(), want)
		}
	})

	t.Run("--version flag", func(t *testing.T) {
		var out bytes.Buffer
		root := newRootCmd(want)
		root.SetOut(&out)
		root.SetArgs([]string{"--version"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), want) {
			t.Errorf("--version 输出 %q 不含注入版本 %q", out.String(), want)
		}
	})
}

// 裸构建（未注入）走占位默认 dev——newRootCmd 收到什么就报什么，这里以 "dev" 代表默认注入位。
func TestVersion_defaultPlaceholder(t *testing.T) {
	var out bytes.Buffer
	root := newRootCmd("dev")
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "dev") {
		t.Errorf("version 输出 %q 不含占位默认 dev", out.String())
	}
}

// `inhomo version` 不应依赖配置：即便 ~/.inhomo/config.yaml 是坏 YAML，也要照常报版本
// （version 子命令用 no-op PersistentPreRunE 跳过 bindConfig）。
func TestVersion_worksWithBrokenConfig(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".inhomo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".inhomo", "config.yaml"),
		[]byte("controller: [坏 YAML\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	var out bytes.Buffer
	root := newRootCmd("v0.9.9-test")
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("坏配置不应影响 version：%v", err)
	}
	if !strings.Contains(out.String(), "v0.9.9-test") {
		t.Errorf("version 输出 %q 不含版本", out.String())
	}
}
