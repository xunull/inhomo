package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// envPrefix 是环境变量前缀：INHOMO_CONTROLLER 覆盖 controller 键、
// INHOMO_TRAFFIC_INTERVAL 覆盖 traffic-interval（连字符键 → 下划线 env），依此类推。
const envPrefix = "INHOMO"

// defaultConfigPath 返回默认配置文件路径 ~/.inhomo/config.yaml（与连接事件库同目录）。
func defaultConfigPath(home string) string {
	return filepath.Join(home, ".inhomo", "config.yaml")
}

// newConfig 组装该命令的已解析配置：绑定 INHOMO_* 环境变量 + 命令 flag，并读配置文件
// （缺失 → 回落默认、不报错；存在但解析失败 → 报错，不静默吞）。
//
// 优先级由 viper 天然给出：显式 flag（flag.Changed）> 环境变量 > 配置文件 > flag 默认（即内置默认）。
// 这正是本项目要的「flags > env > config > 默认」——重访 ADR-0003（controller/secret 曾为 flags-only）。
func newConfig(flags *pflag.FlagSet, cfgFile string) (*viper.Viper, error) {
	v := viper.New()
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()
	if err := v.BindPFlags(flags); err != nil {
		return nil, err
	}
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
		if err := v.ReadInConfig(); err != nil {
			// SetConfigFile 路径缺文件返回 *os.PathError（非 ConfigFileNotFoundError）。
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("读取配置 %s：%w", cfgFile, err)
			}
			// 文件不存在 → 回落默认。
		}
	}
	return v, nil
}

// cfgCtxKey 是把已解析配置挂到命令 context 的私有键。
type cfgCtxKey struct{}

// bindConfig 在 root 的 PersistentPreRunE 里跑：为被调用的子命令建好配置（读 ~/.inhomo/config.yaml
// + env + 该命令全部 flag），挂到 cmd.Context 供各 RunE 用 cfgOf 取。任一命令启动前统一走这里。
func bindConfig(cmd *cobra.Command) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("无法确定 home 目录：%w", err)
	}
	v, err := newConfig(cmd.Flags(), defaultConfigPath(home))
	if err != nil {
		return err
	}
	cmd.SetContext(context.WithValue(cmd.Context(), cfgCtxKey{}, v))
	return nil
}

// cfgOf 取该命令 PersistentPreRunE 建好的已解析配置。各读取点（连接参数/库路径/监听地址等）都经它，
// 而不再直接 cmd.Flags().Get*，从而统一享有「flags > env > config > 默认」的优先级。
//
// 找不到即 panic：所有命令都经 root 的 PersistentPreRunE(bindConfig) 建好配置，到这说明有命令绕过了它
// （如子命令自定义 PersistentPreRunE 覆盖了 root 的）——这是编程错误，应响亮失败，
// 而非静默退化成 flag-only 把 config/env 丢掉（那会悄悄背离本票的优先级语义）。
func cfgOf(cmd *cobra.Command) *viper.Viper {
	v, ok := cmd.Context().Value(cfgCtxKey{}).(*viper.Viper)
	if !ok {
		panic("cfgOf：命令未经 bindConfig（PersistentPreRunE）建配置")
	}
	return v
}
