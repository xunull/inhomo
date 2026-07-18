// Package web 把构建好的前端（web/dist）用 go:embed 打进二进制，供 serve 的 Fiber 托管。
// 注意：go build 需要 web/dist 已存在（本仓库有意提交 dist）；改前端后用 `make frontend` 重建。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist 返回以 dist/ 为根的前端静态资源文件系统。
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
