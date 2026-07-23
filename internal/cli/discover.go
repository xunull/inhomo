package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/xunull/inhomo/internal/logstream"
	"go.yaml.in/yaml/v3"
)

// 零参数自动发现：当 controller 无任何显式来源时，读本机已知客户端（Clash Verge Rev 等）生成的运行时
// mihomo 配置 → 汇成候选控制器 → 逐个用 /version 探活 → 用第一个活的连上；都不活/无配置则回退默认。
// 决策见 ADR-0010；触发条件与「只填空、显式恒赢、all-or-nothing」由 newClient 判定后调这里。

// discovered 是自动发现挑中的一个本机 mihomo 控制器：可直接喂给 logstream.New 的控制器地址 + 其 secret +
// 人类可读的来源客户端标签（source 用于启动行透明打印；secret 绝不打印/记录）。
type discovered struct {
	controller string // TCP "127.0.0.1:9097" 或 unix "unix:///tmp/verge/verge-mihomo.sock"
	secret     string
	source     string
}

// configSource 是一处待探的客户端配置文件：路径 + 人类可读来源标签。
type configSource struct {
	path   string
	source string
}

// probeFunc 探测一个控制器是否可用（/version 返回 200；开了鉴权则所带 secret 也须正确）。作为不纯依赖注入，便于测试。
type probeFunc func(controller, secret string) bool

// mihomoControllerConfig 只取 mihomo 配置里定位 external-controller 所需的三个键。
// Verge 生成的运行时配置与裸 mihomo 配置同此格式（T40 复用同一解析）。
type mihomoControllerConfig struct {
	ExternalController     string `yaml:"external-controller"`
	ExternalControllerUnix string `yaml:"external-controller-unix"`
	Secret                 string `yaml:"secret"`
}

// candidatesFrom 是纯解析接缝：把一份 mihomo 配置解析成有序候选。unix socket 优先（路径固定、更稳、仅本机），
// 其次 TCP。解析失败或两个 controller 键都空 → 返回 nil。secret 随两者一并带出。
func candidatesFrom(data []byte, source string) []discovered {
	var cfg mihomoControllerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	var out []discovered
	if cfg.ExternalControllerUnix != "" {
		// 配置里的 unix 路径以 / 开头 → "unix://" + "/tmp/..." = "unix:///tmp/..."，正是 logstream.New 认的形式。
		out = append(out, discovered{controller: "unix://" + cfg.ExternalControllerUnix, secret: cfg.Secret, source: source})
	}
	if cfg.ExternalController != "" {
		out = append(out, discovered{controller: cfg.ExternalController, secret: cfg.Secret, source: source})
	}
	return out
}

// gatherCandidates 按来源顺序读并解析每处配置，汇成有序候选表；缺失/不可读/解析失败的来源静默跳过
// （守护进程不因某客户端未装而报错）。readFile 注入以便测试。
func gatherCandidates(sources []configSource, readFile func(string) ([]byte, error)) []discovered {
	var out []discovered
	for _, s := range sources {
		data, err := readFile(s.path)
		if err != nil {
			continue
		}
		out = append(out, candidatesFrom(data, s.source)...)
	}
	return out
}

// firstLive 按序探活，返回第一个「活着且 secret 正确」的候选；都不活/空表 → (零值, false)。
func firstLive(cands []discovered, probe probeFunc) (discovered, bool) {
	for _, c := range cands {
		if probe(c.controller, c.secret) {
			return c, true
		}
	}
	return discovered{}, false
}

// applyDiscovery 把发现结果并进最终连接参数并给出一行透明说明：
//   - 发现成功：用发现到的 controller；secret「只填空、显式恒赢」——secret 已显式给则保留显式值，否则用发现值。
//   - 发现失败：保持传入的回退 controller/secret（即内置默认 127.0.0.1:9090），说明为回退提示。
//
// secret 绝不出现在返回的说明里（说明只由 source + controller 拼成）。
func applyDiscovery(fallbackController, secret string, secretExplicit bool, disc discovered, found bool) (string, string, string) {
	if !found {
		return fallbackController, secret, fmt.Sprintf("未发现本机 mihomo，回退默认 controller %s", fallbackController)
	}
	finalSecret := disc.secret
	if secretExplicit {
		finalSecret = secret // 显式 secret 恒赢：只借发现到的 controller
	}
	return disc.controller, finalSecret, fmt.Sprintf("从 %s 自动发现 controller %s", disc.source, disc.controller)
}

// discoverySources 返回本机所有已知客户端的 mihomo 配置候选路径，按固定优先级排列：
// Clash Verge Rev（GUI，v0 主场景）在前，裸 mihomo（~/.config/mihomo）在后；都活时 firstLive 取前者。
// 新增客户端只需往这里再拼一条源，解析器/探活/优先级全复用（ADR-0010）。
func discoverySources(home string) []configSource {
	return append(vergeConfigSources(home), mihomoNativeConfigSources(home)...)
}

// mihomoNativeConfigSources 返回裸 mihomo 的默认配置路径（mihomo -d 的默认目录 ~/.config/mihomo，跨平台一致）。
// 覆盖「被定制过」的裸 mihomo（换了端口 / 设了 secret）；默认 9090 无 secret 的那种由兜底直接覆盖、无需读此。
func mihomoNativeConfigSources(home string) []configSource {
	return []configSource{{path: filepath.Join(home, ".config", "mihomo", "config.yaml"), source: "mihomo"}}
}

// vergeConfigSources 返回本机 Clash Verge Rev 运行时 mihomo 配置的候选路径（按 OS）。
// Verge 把运行时 external-controller/secret 写进它自己 app-data 目录下的 config.yaml。
func vergeConfigSources(home string) []configSource {
	const app = "io.github.clash-verge-rev.clash-verge-rev"
	var dir string
	switch runtime.GOOS {
	case "darwin":
		dir = filepath.Join(home, "Library", "Application Support", app)
	default: // linux 及其它类 unix：Tauri app-data 落在 XDG_DATA_HOME / ~/.local/share
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "share")
		}
		dir = filepath.Join(base, app)
	}
	return []configSource{{path: filepath.Join(dir, "config.yaml"), source: "Clash Verge Rev"}}
}

// hasExplicitSource 判断某 key 是否有显式来源（改过的 flag / INHOMO_ 环境变量 / 配置文件里有该键），
// 以区分「用户/配置指定了值」与「只是 flag 内置默认」。viper 对 bound pflag 的 IsSet 恒真、不可用，故逐源查。
func hasExplicitSource(cmd *cobra.Command, v *viper.Viper, key string) bool {
	if cmd.Flags().Changed(key) {
		return true
	}
	if _, ok := os.LookupEnv(envPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))); ok {
		return true
	}
	return v.InConfig(key)
}

// probeTimeout 是单个候选 /version 探活的超时（本机/socket 探活近乎瞬时，超时只在控制器不可达时才吃满）。
const probeTimeout = time.Second

// livenessProbe 是默认探针：用 logstream 客户端（自动处理 TCP / unix socket 与鉴权）GET /version，200 即活。
func livenessProbe(controller, secret string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return logstream.New(controller, secret).Alive(ctx)
}

// discoverController 在 controller 无显式来源时运行自动发现的整条不纯链路：定位 home → 读客户端配置 →
// 探活 → 按「只填空、显式恒赢」并入结果，返回最终 (controller, secret) 与一行透明说明（发现/回退）。
// 把发现接线收拢到一处（ADR-0010「一处接线」），newClient 只管判触发条件与打印说明。
func discoverController(cmd *cobra.Command, v *viper.Viper, fallbackController, secret string) (string, string, string) {
	secretExplicit := hasExplicitSource(cmd, v, flagSecret)
	home, err := os.UserHomeDir()
	if err != nil {
		return applyDiscovery(fallbackController, secret, secretExplicit, discovered{}, false)
	}
	disc, found := firstLive(gatherCandidates(discoverySources(home), os.ReadFile), livenessProbe)
	return applyDiscovery(fallbackController, secret, secretExplicit, disc, found)
}
