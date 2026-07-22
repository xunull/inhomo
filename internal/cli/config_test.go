package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// testFlags 造一个含代表性 flag 的 flagset：controller（普通 string）+ traffic-interval（含连字符的 Duration）。
func testFlags() *pflag.FlagSet {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("controller", "127.0.0.1:9090", "")
	fs.Duration("traffic-interval", 3*time.Second, "")
	return fs
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("写测试配置：%v", err)
	}
	return p
}

// 优先级是这张票的核心不变量：显式 flag > 环境变量 > 配置文件 > 内置默认。逐档钉死。
func TestNewConfig_precedence(t *testing.T) {
	t.Run("都不设 → flag 默认（内置默认）", func(t *testing.T) {
		v, err := newConfig(testFlags(), "")
		if err != nil {
			t.Fatal(err)
		}
		if got := v.GetString("controller"); got != "127.0.0.1:9090" {
			t.Errorf("controller=%q，想要默认 127.0.0.1:9090", got)
		}
		if got := v.GetDuration("traffic-interval"); got != 3*time.Second {
			t.Errorf("traffic-interval=%v，想要默认 3s", got)
		}
	})

	t.Run("配置文件覆盖默认", func(t *testing.T) {
		cfg := writeConfig(t, "controller: unix:///x.sock\n")
		v, err := newConfig(testFlags(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got := v.GetString("controller"); got != "unix:///x.sock" {
			t.Errorf("controller=%q，想要配置文件值 unix:///x.sock", got)
		}
	})

	t.Run("环境变量胜过配置文件", func(t *testing.T) {
		t.Setenv("INHOMO_CONTROLLER", "envval")
		cfg := writeConfig(t, "controller: unix:///x.sock\n")
		v, err := newConfig(testFlags(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got := v.GetString("controller"); got != "envval" {
			t.Errorf("controller=%q，想要环境变量值 envval（env > config）", got)
		}
	})

	t.Run("显式 flag 胜过环境变量与配置文件", func(t *testing.T) {
		t.Setenv("INHOMO_CONTROLLER", "envval")
		cfg := writeConfig(t, "controller: unix:///x.sock\n")
		fs := testFlags()
		_ = fs.Set("controller", "flagval") // 标记 Changed → viper 视为最高优先
		v, err := newConfig(fs, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got := v.GetString("controller"); got != "flagval" {
			t.Errorf("controller=%q，想要显式 flag 值 flagval（flag > env > config）", got)
		}
	})
}

// 连字符键（traffic-interval）经 INHOMO_TRAFFIC_INTERVAL 覆盖：验证 - → _ 的 env key 替换。
func TestNewConfig_envKeyReplacer(t *testing.T) {
	t.Setenv("INHOMO_TRAFFIC_INTERVAL", "90s")
	v, err := newConfig(testFlags(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := v.GetDuration("traffic-interval"); got != 90*time.Second {
		t.Errorf("traffic-interval=%v，想要环境变量 90s（连字符键 → 下划线 env）", got)
	}
}

// 装配链集成测试：bindConfig（解析 $HOME → 读 ~/.inhomo/config.yaml → 挂 context）→ cfgOf 取回。
// 覆盖 newConfig 纯函数测不到的 cobra glue：确认真从 home 下的 config 读到值、cfgOf 正常取回不 panic。
func TestBindConfig_wiring(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".inhomo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".inhomo", "config.yaml"),
		[]byte("controller: unix:///wired.sock\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home) // os.UserHomeDir 在 unix 读 $HOME

	cmd := &cobra.Command{}
	cmd.Flags().String("controller", "127.0.0.1:9090", "")
	cmd.SetContext(context.Background())
	if err := bindConfig(cmd); err != nil {
		t.Fatalf("bindConfig：%v", err)
	}
	if got := cfgOf(cmd).GetString("controller"); got != "unix:///wired.sock" {
		t.Errorf("cfgOf 经 bindConfig 读到 controller=%q，想要 home 配置里的 unix:///wired.sock", got)
	}
}

// 文件不存在 → 回落默认、不报错；文件存在但解析失败 → 报错（不静默吞）。
func TestNewConfig_fileHandling(t *testing.T) {
	t.Run("文件不存在 → 无错、回落默认", func(t *testing.T) {
		v, err := newConfig(testFlags(), filepath.Join(t.TempDir(), "nope.yaml"))
		if err != nil {
			t.Fatalf("缺文件不应报错：%v", err)
		}
		if got := v.GetString("controller"); got != "127.0.0.1:9090" {
			t.Errorf("controller=%q，想要默认", got)
		}
	})

	t.Run("非法 YAML → 报错", func(t *testing.T) {
		bad := writeConfig(t, "controller: [未闭合\n")
		if _, err := newConfig(testFlags(), bad); err == nil {
			t.Error("非法配置应报错，却成功了")
		}
	})
}
