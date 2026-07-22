// inhomo：审计经由 mihomo 出站的明文 HTTP 泄露。
// 命令行入口，子命令见 internal/cli。
package main

import "github.com/xunull/inhomo/internal/cli"

// version 是构建期注入位：裸构建为 "dev"，发布经 ldflags `-X main.version=<tag>` 注入具体版本
// （由「打包分发」的 goreleaser 流水线注入）。
var version = "dev"

func main() {
	cli.Execute(version)
}
