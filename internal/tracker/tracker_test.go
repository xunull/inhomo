package tracker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// sampleDomainMap 是 DuckDuckGo Tracker Radar `domain_map.json` 的形态 fixture：域名 → 归属实体。
// 键是已注册域名（eTLD+1），值带 displayName / entityName。
const sampleDomainMap = `{
  "google-analytics.com": {"displayName": "Google", "entityName": "Google LLC", "aliases": []},
  "doubleclick.net": {"displayName": "Google", "entityName": "Google LLC"},
  "scorecardresearch.com": {"displayName": "comScore", "entityName": "comScore, Inc."},
  "entityname-only.com": {"entityName": "Fallback Owner"}
}`

// TestParseAndClassify 是纯解析 + 归类接缝：一份 domain_map → 按 host 的 eTLD+1 查归属。
func TestParseAndClassify(t *testing.T) {
	c, err := Parse([]byte(sampleDomainMap))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		host      string
		wantOwner string
		wantKnown bool
	}{
		{"www.google-analytics.com", "Google", true}, // 子域名归到 eTLD+1 命中
		{"google-analytics.com", "Google", true},
		{"scorecardresearch.com", "comScore", true},
		{"entityname-only.com", "Fallback Owner", true}, // 无 displayName 回落 entityName
		{"graph.facebook.com", "", false},               // 不在表里
		{"example.com", "", false},
		{"1.2.3.4", "", false},   // IP 无 eTLD+1
		{"localhost", "", false}, // 单标签无 eTLD+1
		{"", "", false},
	}
	for _, tc := range cases {
		owner, known := c.Classify(tc.host)
		if owner != tc.wantOwner || known != tc.wantKnown {
			t.Errorf("Classify(%q)=(%q,%v)，期望 (%q,%v)", tc.host, owner, known, tc.wantOwner, tc.wantKnown)
		}
	}
}

// TestClassify_nilAndEmpty：nil / 空归类器全部 unknown、不 panic（数据集未拉取时的优雅退化）。
func TestClassify_nilAndEmpty(t *testing.T) {
	var c *Classifier
	if owner, known := c.Classify("google-analytics.com"); known || owner != "" {
		t.Error("nil Classifier 应全部 unknown、不 panic")
	}
	empty, err := Parse([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, known := empty.Classify("google-analytics.com"); known {
		t.Error("空数据集应全部 unknown")
	}
}

// TestParse_malformed：非法 JSON 报错（由 Load 决定是否吞成空）。
func TestParse_malformed(t *testing.T) {
	if _, err := Parse([]byte(`{not json`)); err == nil {
		t.Error("非法 JSON 应报错")
	}
}

// TestLoad：本地缓存存在则解析；缺失 → 空归类器 + 无错（数据集未拉取时不报错、全 unknown）。
func TestLoad(t *testing.T) {
	dir := t.TempDir()

	t.Run("缺文件 → 空归类器、无错", func(t *testing.T) {
		c, err := Load(filepath.Join(dir, "nope.json"))
		if err != nil {
			t.Fatalf("缺文件不应报错：%v", err)
		}
		if _, known := c.Classify("google-analytics.com"); known {
			t.Error("缺数据集应全部 unknown")
		}
	})

	t.Run("有文件 → 正常解析", func(t *testing.T) {
		p := filepath.Join(dir, "tr.json")
		if err := os.WriteFile(p, []byte(sampleDomainMap), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if owner, known := c.Classify("www.doubleclick.net"); !known || owner != "Google" {
			t.Errorf("Load 后 Classify(doubleclick)=(%q,%v)，期望 (Google,true)", owner, known)
		}
	})
}

// TestFetch：200 → 建目录、原子写入、可 Load 回来；非 200 → 报错且不留半截目标文件。
func TestFetch(t *testing.T) {
	t.Run("200 → 写入并可 Load", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(sampleDomainMap))
		}))
		defer ts.Close()
		dest := filepath.Join(t.TempDir(), "sub", "tr.json") // 目录不存在 → 验证 MkdirAll
		if err := Fetch(context.Background(), ts.URL, dest); err != nil {
			t.Fatal(err)
		}
		c, err := Load(dest)
		if err != nil {
			t.Fatal(err)
		}
		if owner, known := c.Classify("google-analytics.com"); !known || owner != "Google" {
			t.Errorf("Fetch 后 Load 应能归类，得 (%q,%v)", owner, known)
		}
	})

	t.Run("非 200 → 报错、不留目标文件", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()
		dest := filepath.Join(t.TempDir(), "tr.json")
		if err := Fetch(context.Background(), ts.URL, dest); err == nil {
			t.Error("非 200 应报错")
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Error("失败不应留下目标文件")
		}
	})
}
