// brewgen 从 Release 的 checksums.txt 生成 Homebrew formula（inhomo.rb），供 tap 分发预编译二进制。
//
// 为何是独立工具而非主二进制的子命令：它只在发布时（CI）或本地预演时跑，不该进用户装的 inhomo 里。
// 放 tools/ 下、只被 `go run ./tools/brewgen` 调用，主二进制 `go build .` 不会内联它。
//
//	go run ./tools/brewgen -version v0.1.0 -checksums dist/checksums.txt -out dist/inhomo.rb
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/template"
)

// target 是发布矩阵的一个平台。goos/goarch 用来拼资产名，与 .github/workflows/release.yml、
// Makefile 的 `inhomo_<tag>_<goos>_<goarch>.tar.gz` 命名一致。renderFormula 逐一取这 4 个目标。
type target struct {
	goos, goarch string
}

func assetName(tag string, t target) string {
	return fmt.Sprintf("inhomo_%s_%s_%s.tar.gz", tag, t.goos, t.goarch)
}

func assetURL(tag, name string) string {
	return fmt.Sprintf("https://github.com/xunull/inhomo/releases/download/%s/%s", tag, name)
}

// parseChecksums 把 `sha256sum` 风格的 checksums.txt（GNU 文本模式 `<hex>  <文件名>`，双空格）
// 解析成 文件名 → sha256 的映射。空行与行尾空白容忍；非空但字段不足的行视为坏数据报错。
// 注：只认文本模式的文件名；若是二进制模式（`<hex> *<文件名>`）会带上 `*` 前缀而对不上，
// 届时 renderFormula 查不到目标即 fail-closed 报错，不会静默产出错 formula。
func parseChecksums(data []byte) (map[string]string, error) {
	sums := make(map[string]string)
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("checksums 坏行（字段不足）：%q", line)
		}
		sums[fields[1]] = fields[0]
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return sums, nil
}

type asset struct{ URL, SHA string }

type formulaData struct {
	Version                                  string
	DarwinArm, DarwinAmd, LinuxArm, LinuxAmd asset
}

// renderFormula 依据 tag 与 文件名→sha 映射渲染 formula。fail-closed：任一目标缺 sha 即报错，
// 绝不产出缺目标的 formula（那会让某平台的用户 brew install 失败或装到旧版）。
func renderFormula(tag string, sums map[string]string) (string, error) {
	if tag == "" {
		return "", fmt.Errorf("tag 为空")
	}
	// 发布矩阵的 4 个目标，逐一解析 sha；缺任一即 fail-closed。
	assets := make(map[target]asset, 4)
	for _, t := range []target{
		{"darwin", "arm64"}, {"darwin", "amd64"},
		{"linux", "arm64"}, {"linux", "amd64"},
	} {
		name := assetName(tag, t)
		sha, ok := sums[name]
		if !ok || sha == "" {
			return "", fmt.Errorf("checksums 缺少 %s 的 sha256", name)
		}
		assets[t] = asset{URL: assetURL(tag, name), SHA: sha}
	}

	// 摊到模板的扁平字段（模板按 OS→arch 分组，扁平字段让模板保持一目了然）。
	data := formulaData{
		Version:   strings.TrimPrefix(tag, "v"),
		DarwinArm: assets[target{"darwin", "arm64"}],
		DarwinAmd: assets[target{"darwin", "amd64"}],
		LinuxArm:  assets[target{"linux", "arm64"}],
		LinuxAmd:  assets[target{"linux", "amd64"}],
	}
	var buf bytes.Buffer
	if err := formulaTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

var formulaTmpl = template.Must(template.New("formula").Parse(`# 本文件由 tools/brewgen 依据 Release 的 checksums.txt 生成，请勿手改。
# 更新方式：发布流水线（.github/workflows/release.yml）打 tag 时重新生成并附到 Release。
class Inhomo < Formula
  desc "Audit plaintext HTTP leaks through mihomo egress proxy nodes"
  homepage "https://github.com/xunull/inhomo"
  version "{{.Version}}"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "{{.DarwinArm.URL}}"
      sha256 "{{.DarwinArm.SHA}}"
    end
    on_intel do
      url "{{.DarwinAmd.URL}}"
      sha256 "{{.DarwinAmd.SHA}}"
    end
  end

  on_linux do
    on_arm do
      url "{{.LinuxArm.URL}}"
      sha256 "{{.LinuxArm.SHA}}"
    end
    on_intel do
      url "{{.LinuxAmd.URL}}"
      sha256 "{{.LinuxAmd.SHA}}"
    end
  end

  # Homebrew 用自带 curl 下载，不打 com.apple.quarantine 隔离标记（那是浏览器下载才加的），
  # 故未签名二进制经 brew 安装后可直接运行，无需手动清 xattr。v0 不做 Apple 公证。
  def install
    bin.install "inhomo"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/inhomo version")
  end
end
`))

func main() {
	version := flag.String("version", "", "release tag，如 v0.1.0（url 用带 v 的，brew version 自动去 v）")
	checksums := flag.String("checksums", "", "checksums.txt 路径")
	out := flag.String("out", "", "输出 formula 路径（留空 = 打到 stdout）")
	flag.Parse()

	if *version == "" || *checksums == "" {
		fmt.Fprintln(os.Stderr, "用法：brewgen -version <tag> -checksums <path> [-out <path>]")
		os.Exit(2)
	}

	data, err := os.ReadFile(*checksums)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读 checksums：%v\n", err)
		os.Exit(1)
	}
	sums, err := parseChecksums(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 checksums：%v\n", err)
		os.Exit(1)
	}
	formula, err := renderFormula(*version, sums)
	if err != nil {
		fmt.Fprintf(os.Stderr, "生成 formula：%v\n", err)
		os.Exit(1)
	}

	if *out == "" {
		fmt.Print(formula)
		return
	}
	if err := os.WriteFile(*out, []byte(formula), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写 formula：%v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "→ %s\n", *out)
}
