package langstat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree 按 map（相对路径 → 内容）在 root 下落盘文件。
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// expectDecision 是对单个文件解释结果的断言。
type expectDecision struct {
	included bool
	reason   ReasonKind
	ruleFile string // 为空表示不校验
	line     int    // 为 0 表示不校验
}

// runScenario 执行统计并逐文件核对解释与人工列出的纳入清单。
// included 为人工列出的纳入源码文件（相对路径 → 内容长度）。
func runScenario(t *testing.T, files map[string]string, included map[string]int, decisions map[string]expectDecision) *Report {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, files)
	a := NewAnalyzer(root)

	rep, err := a.Analyze()
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	// 语言字节总量须与人工清单完全相符。
	wantLangBytes := map[string]int64{}
	wantLangFiles := map[string]int{}
	var wantTotal int64
	for rel, size := range included {
		ext := filepath.Ext(rel)
		wantLangBytes[ext] += int64(size)
		wantLangFiles[ext]++
		wantTotal += int64(size)
	}
	if rep.TotalBytes != wantTotal {
		t.Errorf("TotalBytes = %d, want %d", rep.TotalBytes, wantTotal)
	}
	if rep.TotalFiles != len(included) {
		t.Errorf("TotalFiles = %d, want %d", rep.TotalFiles, len(included))
	}
	if len(rep.Languages) != len(wantLangBytes) {
		t.Errorf("语言数 = %d, want %d", len(rep.Languages), len(wantLangBytes))
	}
	for ext, ls := range rep.Languages {
		if ls.Bytes != wantLangBytes[ext] {
			t.Errorf("%s Bytes = %d, want %d", ext, ls.Bytes, wantLangBytes[ext])
		}
		if ls.Files != wantLangFiles[ext] {
			t.Errorf("%s Files = %d, want %d", ext, ls.Files, wantLangFiles[ext])
		}
		if wantTotal > 0 {
			wantPct := float64(wantLangBytes[ext]) * 100 / float64(wantTotal)
			if ls.Percent != wantPct {
				t.Errorf("%s Percent = %v, want %v", ext, ls.Percent, wantPct)
			}
		}
	}

	// 每个源码文件的解释必须与实际统计一致。
	for rel, content := range files {
		ext := filepath.Ext(rel)
		if !sourceExts[ext] {
			continue
		}
		d, err := a.Explain(rel)
		if err != nil {
			t.Fatalf("Explain(%s): %v", rel, err)
		}
		_, wantIn := included[rel]
		if d.Included != wantIn {
			t.Errorf("Explain(%s).Included = %v, want %v", rel, d.Included, wantIn)
		}
		if exp, ok := decisions[rel]; ok {
			if d.Reason != exp.reason {
				t.Errorf("Explain(%s).Reason = %v, want %v", rel, d.Reason, exp.reason)
			}
			if exp.ruleFile != "" && d.RuleFile != exp.ruleFile {
				t.Errorf("Explain(%s).RuleFile = %q, want %q", rel, d.RuleFile, exp.ruleFile)
			}
			if exp.line != 0 && d.Line != exp.line {
				t.Errorf("Explain(%s).Line = %d, want %d", rel, d.Line, exp.line)
			}
		}
		_ = content
	}
	return rep
}

// 祖先屏蔽：仅反选子文件不能越过被排除的目录。
func TestAncestorMasking(t *testing.T) {
	files := map[string]string{
		".gitignore":      "gen/\n",
		"main.go":         strings.Repeat("a", 10),
		"gen/.gitignore":  "!keep.go\n",
		"gen/keep.go":     strings.Repeat("b", 20),
		"gen/drop.go":     strings.Repeat("c", 30),
		"gen/sub/deep.go": strings.Repeat("d", 40),
	}
	included := map[string]int{"main.go": 10}
	decisions := map[string]expectDecision{
		"main.go":     {true, ReasonDefaultInclude, "", 0},
		"gen/keep.go": {false, ReasonAncestorExcluded, ".gitignore", 1},
		"gen/drop.go": {false, ReasonAncestorExcluded, ".gitignore", 1},
		// 解释必须指向祖先的排除规则，而非尚不可见的文件级反选规则。
		"gen/sub/deep.go": {false, ReasonAncestorExcluded, ".gitignore", 1},
	}
	runScenario(t, files, included, decisions)
}

// 恢复沿途目录后，深层规则才生效，子文件可被重新纳入。
func TestReincludeAfterRestoringDirs(t *testing.T) {
	files := map[string]string{
		".gitignore":     "gen/\n!gen/\n",
		"main.go":        strings.Repeat("a", 10),
		"gen/.gitignore": "*\n!keep.go\n",
		"gen/keep.go":    strings.Repeat("b", 20),
		"gen/drop.go":    strings.Repeat("c", 30),
	}
	included := map[string]int{"main.go": 10, "gen/keep.go": 20}
	decisions := map[string]expectDecision{
		"main.go":     {true, ReasonDefaultInclude, "", 0},
		"gen/keep.go": {true, ReasonMatchedRule, "gen/.gitignore", 2},
		"gen/drop.go": {false, ReasonMatchedRule, "gen/.gitignore", 1},
	}
	runScenario(t, files, included, decisions)
}

// 嵌套覆盖：同文件后面的规则覆盖前面的，较深目录的规则覆盖祖先的。
func TestNestedOverride(t *testing.T) {
	files := map[string]string{
		".gitignore":     "*.js\n!app.js\n",
		"app.js":         strings.Repeat("a", 5),
		"x.js":           strings.Repeat("b", 6),
		"sub/.gitignore": "!x.js\n",
		"sub/x.js":       strings.Repeat("c", 7),
		"sub/y.js":       strings.Repeat("d", 8),
		"sub/z.ts":       strings.Repeat("e", 9),
	}
	included := map[string]int{"app.js": 5, "sub/x.js": 7, "sub/z.ts": 9}
	decisions := map[string]expectDecision{
		"app.js":   {true, ReasonMatchedRule, ".gitignore", 2},
		"x.js":     {false, ReasonMatchedRule, ".gitignore", 1},
		"sub/x.js": {true, ReasonMatchedRule, "sub/.gitignore", 1},
		"sub/y.js": {false, ReasonMatchedRule, ".gitignore", 1},
		"sub/z.ts": {true, ReasonDefaultInclude, "", 0},
	}
	runScenario(t, files, included, decisions)
}

// 双星：零层匹配；末尾双星不排除目录本身，其 .gitignore 仍然可见。
func TestDoubleStarSemantics(t *testing.T) {
	files := map[string]string{
		".gitignore":      "a/**/gen_*.py\nlogs/**\n",
		"a/gen_x.py":      strings.Repeat("a", 10), // 零层匹配
		"a/deep/gen_y.py": strings.Repeat("b", 20), // 多层匹配
		"a/ok.py":         strings.Repeat("c", 30),
		"logs/.gitignore": "!keep.py\n",
		"logs/keep.py":    strings.Repeat("d", 40), // logs 本身未排除，反选生效
		"logs/drop.py":    strings.Repeat("e", 50),
		"keep.go":         strings.Repeat("f", 60),
	}
	included := map[string]int{"a/ok.py": 30, "logs/keep.py": 40, "keep.go": 60}
	decisions := map[string]expectDecision{
		"a/gen_x.py":      {false, ReasonMatchedRule, ".gitignore", 1},
		"a/deep/gen_y.py": {false, ReasonMatchedRule, ".gitignore", 1},
		"a/ok.py":         {true, ReasonDefaultInclude, "", 0},
		"logs/keep.py":    {true, ReasonMatchedRule, "logs/.gitignore", 1},
		"logs/drop.py":    {false, ReasonMatchedRule, ".gitignore", 2},
		"keep.go":         {true, ReasonDefaultInclude, "", 0},
	}
	runScenario(t, files, included, decisions)
}

// 转义空格与 CRLF 混用。
func TestEscapedSpaceAndCRLF(t *testing.T) {
	files := map[string]string{
		// CRLF 行尾；含转义井号、转义空格、普通含空格规则。
		".gitignore":   "my file.py\r\n\\#hash.py\r\ntrail\\ .py\r\nspace\\ \r\n",
		"my file.py":   strings.Repeat("a", 10),
		"#hash.py":     strings.Repeat("b", 20),
		"trail .py":    strings.Repeat("c", 30),
		"trail.py":     strings.Repeat("d", 40), // 转义空格不匹配无空格名
		"space ":       strings.Repeat("e", 50), // 行尾转义空格（非源码文件）
		"ok.go":        strings.Repeat("f", 60),
		"unrelated.py": strings.Repeat("g", 70),
	}
	included := map[string]int{"trail.py": 40, "ok.go": 60, "unrelated.py": 70}
	decisions := map[string]expectDecision{
		"my file.py":   {false, ReasonMatchedRule, ".gitignore", 1},
		"#hash.py":     {false, ReasonMatchedRule, ".gitignore", 2},
		"trail .py":    {false, ReasonMatchedRule, ".gitignore", 3},
		"trail.py":     {true, ReasonDefaultInclude, "", 0},
		"ok.go":        {true, ReasonDefaultInclude, "", 0},
		"unrelated.py": {true, ReasonDefaultInclude, "", 0},
	}
	root := t.TempDir()
	writeTree(t, root, files)
	a := NewAnalyzer(root)
	runScenario(t, files, included, decisions)

	// 非源码文件也可解释：行尾转义空格规则命中 "space "。
	d, err := a.Explain("space ")
	if err != nil {
		t.Fatalf("Explain(space ): %v", err)
	}
	if d.Included || d.Reason != ReasonMatchedRule || d.Line != 4 {
		t.Errorf("Explain(space ) = %+v, 应被第 4 行排除", d)
	}
}

// 不存在的文件与非法路径返回错误。
func TestExplainErrors(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.go": "x"})
	a := NewAnalyzer(root)
	if _, err := a.Explain("missing.go"); err == nil {
		t.Error("不存在的文件应返回错误")
	}
	if _, err := a.Explain("../outside.go"); err == nil {
		t.Error("越界路径应返回错误")
	}
}
