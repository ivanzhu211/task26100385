package langstat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixture 按 path → 内容 创建测试目录树。
func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// check 统计 root 并校验：每个源码文件的 Explain 结果与 wantIncluded 一致，
// 语言字节总量与人工清单完全相符。
func check(t *testing.T, root string, wantIncluded map[string]bool) *Analyzer {
	t.Helper()
	a, err := Analyze(root)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	// 遍历目录树，逐文件核对 Explain 与统计一致性。
	wantBytes := map[string]int64{}
	var wantTotal int64
	var wantFiles int
	seen := map[string]bool{}
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		ext := filepath.Ext(rel)
		if _, ok := langByExt[ext]; !ok {
			return nil
		}
		want, listed := wantIncluded[rel]
		if !listed {
			t.Errorf("文件 %s 未在期望清单中列出", rel)
			return nil
		}
		seen[rel] = true
		exp, err := a.Explain(rel)
		if err != nil {
			t.Errorf("Explain(%s): %v", rel, err)
			return nil
		}
		if exp.Included != want {
			t.Errorf("Explain(%s).Included = %v，期望 %v（规则 %s:%d）",
				rel, exp.Included, want, exp.RuleFile, exp.RuleLine)
		}
		if exp.Counted != want {
			t.Errorf("Explain(%s).Counted = %v，期望 %v", rel, exp.Counted, want)
		}
		if want {
			info, _ := os.Stat(p)
			wantBytes[langByExt[ext]] += info.Size()
			wantTotal += info.Size()
			wantFiles++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel := range wantIncluded {
		if !seen[rel] {
			t.Errorf("期望清单中的 %s 在磁盘上不存在", rel)
		}
	}
	rep := a.Report()
	if rep.TotalBytes != wantTotal {
		t.Errorf("TotalBytes = %d，期望 %d", rep.TotalBytes, wantTotal)
	}
	if rep.Files != wantFiles {
		t.Errorf("Files = %d，期望 %d", rep.Files, wantFiles)
	}
	gotBytes := map[string]int64{}
	var sum int64
	for _, ls := range rep.Languages {
		gotBytes[ls.Language] = ls.Bytes
		sum += ls.Bytes
		if wantTotal > 0 {
			wantRatio := float64(wantBytes[ls.Language]) / float64(wantTotal)
			if ls.Ratio < wantRatio-1e-12 || ls.Ratio > wantRatio+1e-12 {
				t.Errorf("%s Ratio = %v，期望 %v", ls.Language, ls.Ratio, wantRatio)
			}
		}
	}
	if sum != rep.TotalBytes {
		t.Errorf("各语言字节和 %d 与 TotalBytes %d 不一致", sum, rep.TotalBytes)
	}
	for lang, wb := range wantBytes {
		if gotBytes[lang] != wb {
			t.Errorf("%s 字节 = %d，期望 %d", lang, gotBytes[lang], wb)
		}
	}
	return a
}

func body(n int) string { return strings.Repeat("x", n) }

// 嵌套规则覆盖与祖先屏蔽：仅反选子文件仍被排除。
func TestAncestorShielding(t *testing.T) {
	root := writeFixture(t, map[string]string{
		".gitignore":     "gen/\n!gen/keep.py\n", // 反选被屏蔽目录内的文件，无效
		"gen/.gitignore": "!keep.py\n",           // 被排除目录内的 .gitignore 不参与
		"gen/keep.py":    body(100),
		"gen/other.py":   body(200),
		"src/app.py":     body(300),
		"src/app.js":     body(400),
	})
	a := check(t, root, map[string]bool{
		"gen/keep.py":  false,
		"gen/other.py": false,
		"src/app.py":   true,
		"src/app.js":   true,
	})
	// 解释必须指向祖先目录的排除规则，而非不可见的文件级反选。
	exp, err := a.Explain("gen/keep.py")
	if err != nil {
		t.Fatal(err)
	}
	if exp.Included || exp.RuleFile != ".gitignore" || exp.RuleLine != 1 || exp.RuleText != "gen/" {
		t.Errorf("屏蔽解释错误: %+v", exp)
	}
}

// 恢复沿途目录后，深层反选才生效。
func TestReincludeAlongPath(t *testing.T) {
	root := writeFixture(t, map[string]string{
		".gitignore":         "out/\n!out/\n",
		"out/.gitignore":     "*\n!sub/\n",
		"out/a.py":           body(10), // 被 out/.gitignore 的 * 排除
		"out/sub/.gitignore": "!b.py\n",
		"out/sub/b.py":       body(20), // 重新纳入
		"out/sub/c.go":       body(40), // 仍被 * 排除
		"top.ts":             body(80),
	})
	a := check(t, root, map[string]bool{
		"out/a.py":     false,
		"out/sub/b.py": true,
		"out/sub/c.go": false,
		"top.ts":       true,
	})
	exp, err := a.Explain("out/sub/b.py")
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Included || exp.RuleFile != "out/sub/.gitignore" || exp.RuleLine != 1 {
		t.Errorf("反选解释错误: %+v", exp)
	}
	exp, err = a.Explain("out/a.py")
	if err != nil {
		t.Fatal(err)
	}
	if exp.Included || exp.RuleFile != "out/.gitignore" || exp.RuleLine != 1 || exp.RuleText != "*" {
		t.Errorf("排除解释错误: %+v", exp)
	}
}

// 未恢复沿途目录时，深层反选不能越过屏蔽。
func TestDeepShielding(t *testing.T) {
	root := writeFixture(t, map[string]string{
		".gitignore":     "a/\n",
		"a/.gitignore":   "!b/\n",   // 不可见
		"a/b/.gitignore": "!c.py\n", // 不可见
		"a/b/c.py":       body(50),
		"visible/d.py":   body(60),
	})
	a := check(t, root, map[string]bool{
		"a/b/c.py":     false,
		"visible/d.py": true,
	})
	exp, err := a.Explain("a/b/c.py")
	if err != nil {
		t.Fatal(err)
	}
	if exp.Included || exp.RuleFile != ".gitignore" || exp.RuleLine != 1 {
		t.Errorf("深层屏蔽解释错误: %+v", exp)
	}
}

// 双星：零层匹配与多层匹配。
func TestDoubleStar(t *testing.T) {
	root := writeFixture(t, map[string]string{
		".gitignore":        "**/gen/*.py\n",
		"gen/a.py":          body(10), // ** 零层
		"x/gen/b.py":        body(20),
		"x/y/gen/c.py":      body(30),
		"x/y/gen/deep/d.py": body(40), // * 不跨层，不排除
		"x/y/gen/deep/e.go": body(50),
	})
	check(t, root, map[string]bool{
		"gen/a.py":          false,
		"x/gen/b.py":        false,
		"x/y/gen/c.py":      false,
		"x/y/gen/deep/d.py": true,
		"x/y/gen/deep/e.go": true,
	})
}

// 末尾 /** 匹配目录内所有层级条目，但不匹配目录本身。
func TestTrailingDoubleStar(t *testing.T) {
	root := writeFixture(t, map[string]string{
		".gitignore":          "assets/**\n",
		"assets/.gitignore":   "!/one.py\n", // 目录本身未被排除，此文件生效
		"assets/one.py":       body(10),
		"assets/sub/two.py":   body(20),
		"assets/sub/three.js": body(30),
	})
	// assets 目录本身未被排除，遍历进入后其 .gitignore 的反选恢复 one.py；
	// sub 被 ** 排除，其内部不可恢复。
	a := check(t, root, map[string]bool{
		"assets/one.py":       true,
		"assets/sub/two.py":   false,
		"assets/sub/three.js": false,
	})
	exp, err := a.Explain("assets/sub/two.py")
	if err != nil {
		t.Fatal(err)
	}
	if exp.Included || exp.RuleFile != ".gitignore" || exp.RuleLine != 1 {
		t.Errorf("屏蔽解释错误: %+v", exp)
	}
}

// 转义空格与 CRLF 混用。
func TestEscapedSpaceAndCRLF(t *testing.T) {
	root := writeFixture(t, map[string]string{
		".gitignore":   "my file.py\r\nesc\\ space.py\r\ntrail.py   \r\nkeep\\ .py\r\n# 注释\r\n\r\n",
		"my file.py":   body(10),
		"esc space.py": body(20),
		"trail.py":     body(30),
		"keep .py":     body(40),
		"keep.py":      body(50),
		"other.go":     body(60),
	})
	check(t, root, map[string]bool{
		"my file.py":   false,
		"esc space.py": false,
		"trail.py":     false,
		"keep .py":     false,
		"keep.py":      true,
		"other.go":     true,
	})
}

// 无规则命中时默认纳入；.gitignore 忽略自身不影响其生效。
func TestDefaultAndSelfIgnore(t *testing.T) {
	root := writeFixture(t, map[string]string{
		".gitignore":     ".gitignore\nskip.js\n",
		".gitignore.bak": body(999),
		"skip.js":        body(10),
		"main.go":        body(20),
		"README.md":      body(30), // 非源码扩展名，不计入
	})
	a := check(t, root, map[string]bool{
		"skip.js": false,
		"main.go": true,
	})
	exp, err := a.Explain("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Included || !exp.Default {
		t.Errorf("默认纳入解释错误: %+v", exp)
	}
	// 非源码扩展名：不被忽略规则排除，但不计入统计。
	exp, err = a.Explain("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Included || exp.Counted {
		t.Errorf("非源码文件解释错误: %+v", exp)
	}
}

// 同文件内后面的规则覆盖前面的规则；较深目录规则覆盖祖先规则。
func TestLaterRuleWins(t *testing.T) {
	root := writeFixture(t, map[string]string{
		".gitignore":     "*.py\n!a.py\na.py\n",
		"a.py":           body(10),
		"b.py":           body(20),
		"sub/.gitignore": "!*.py\n",
		"sub/c.py":       body(30),
	})
	check(t, root, map[string]bool{
		"a.py":     false, // 第 3 行覆盖第 2 行
		"b.py":     false,
		"sub/c.py": true, // 较深目录反选覆盖祖先
	})
}
