package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 一份 4 目标齐全的 checksums.txt（sha256sum 双空格格式），sha 用 64 位 16 进制占位。
const sampleChecksums = `1111111111111111111111111111111111111111111111111111111111111111  inhomo_v0.1.0_darwin_arm64.tar.gz
2222222222222222222222222222222222222222222222222222222222222222  inhomo_v0.1.0_darwin_amd64.tar.gz
3333333333333333333333333333333333333333333333333333333333333333  inhomo_v0.1.0_linux_arm64.tar.gz
4444444444444444444444444444444444444444444444444444444444444444  inhomo_v0.1.0_linux_amd64.tar.gz
`

func TestParseChecksums_mapsFilenameToSha(t *testing.T) {
	sums, err := parseChecksums([]byte(sampleChecksums))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"inhomo_v0.1.0_darwin_arm64.tar.gz": "1111111111111111111111111111111111111111111111111111111111111111",
		"inhomo_v0.1.0_darwin_amd64.tar.gz": "2222222222222222222222222222222222222222222222222222222222222222",
		"inhomo_v0.1.0_linux_arm64.tar.gz":  "3333333333333333333333333333333333333333333333333333333333333333",
		"inhomo_v0.1.0_linux_amd64.tar.gz":  "4444444444444444444444444444444444444444444444444444444444444444",
	}
	if len(sums) != len(want) {
		t.Fatalf("解析出 %d 条，期望 %d 条：%v", len(sums), len(want), sums)
	}
	for name, sha := range want {
		if sums[name] != sha {
			t.Errorf("%s → %q，期望 %q", name, sums[name], sha)
		}
	}
}

// 空行、行尾空白应被容忍；只有 1 个字段的非空行是坏数据，要报错。
func TestParseChecksums_toleratesBlankLinesRejectsMalformed(t *testing.T) {
	ok := "\n1111111111111111111111111111111111111111111111111111111111111111  a.tar.gz  \n\n"
	if _, err := parseChecksums([]byte(ok)); err != nil {
		t.Fatalf("含空行/行尾空白不应报错：%v", err)
	}
	if _, err := parseChecksums([]byte("deadbeef\n")); err == nil {
		t.Error("单字段坏行应报错，实际未报")
	}
}

func TestRenderFormula_containsAllTargets(t *testing.T) {
	sums, err := parseChecksums([]byte(sampleChecksums))
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderFormula("v0.1.0", sums)
	if err != nil {
		t.Fatal(err)
	}

	// brew 的 version 去掉前导 v，但 url/tag 仍带 v。
	for _, s := range []string{
		`class Inhomo < Formula`,
		`version "0.1.0"`,
		`license "Apache-2.0"`,
		`https://github.com/xunull/inhomo/releases/download/v0.1.0/inhomo_v0.1.0_darwin_arm64.tar.gz`,
		`https://github.com/xunull/inhomo/releases/download/v0.1.0/inhomo_v0.1.0_linux_amd64.tar.gz`,
		`sha256 "1111111111111111111111111111111111111111111111111111111111111111"`,
		`sha256 "4444444444444444444444444444444444444444444444444444444444444444"`,
		`bin.install "inhomo"`,
	} {
		if !strings.Contains(out, s) {
			t.Errorf("渲染结果缺少 %q\n---\n%s", s, out)
		}
	}
	// 每个目标一段 sha256，共 4 段。
	if n := strings.Count(out, "sha256 "); n != 4 {
		t.Errorf("sha256 段数 = %d，期望 4", n)
	}
}

// 缺任一平台的 sha 都应 fail-closed：宁可不出 formula，也不出缺目标的 formula。
func TestRenderFormula_failsOnMissingTarget(t *testing.T) {
	sums, err := parseChecksums([]byte(sampleChecksums))
	if err != nil {
		t.Fatal(err)
	}
	delete(sums, "inhomo_v0.1.0_linux_arm64.tar.gz")
	if _, err := renderFormula("v0.1.0", sums); err == nil {
		t.Error("缺 linux/arm64 应报错，实际未报")
	}
}

func TestRenderFormula_rejectsEmptyTag(t *testing.T) {
	if _, err := renderFormula("", map[string]string{}); err == nil {
		t.Error("空 tag 应报错")
	}
}

// 渲染产物必须是合法 Ruby 语法（AC5 的 `ruby -c`）。ruby 不在则跳过。
func TestRenderFormula_isValidRuby(t *testing.T) {
	ruby, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("无 ruby，跳过语法校验")
	}
	sums, err := parseChecksums([]byte(sampleChecksums))
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderFormula("v0.1.0", sums)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "inhomo.rb")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := exec.Command(ruby, "-c", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ruby -c 失败：%v\n%s", err, res)
	}
	if !strings.Contains(string(res), "Syntax OK") {
		t.Errorf("ruby -c 未通过：%s", res)
	}
}
