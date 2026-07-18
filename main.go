// inhomo：审计经由 mihomo 出站的明文 HTTP 泄露。
// 命令行入口，子命令见 internal/cli。
package main

import "github.com/xunull/inhomo/internal/cli"

func main() {
	cli.Execute()
}
