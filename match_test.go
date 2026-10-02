package langstat

import "testing"

func seg(t *testing.T, pat string) []token {
	t.Helper()
	p := parseLine(pat, 1)
	if p == nil || len(p.segs) != 1 || p.segs[0].doubleStar {
		t.Fatalf("%q 不是单段普通规则", pat)
	}
	return p.segs[0].toks
}

func TestMatchSegment(t *testing.T) {
	cases := []struct {
		pat, name string
		want      bool
	}{
		{"*", "", true},   // 单星匹配零个字符
		{"*", "ab", true}, // 单星匹配多个非斜杠字符
		{"a*c", "ac", true},
		{"a*c", "abc", true},
		{"a*c", "ab", false},
		{"?", "a", true}, // 问号匹配一个字符
		{"?", "ab", false},
		{"a?c", "abc", true},
		{"[abc]", "b", true},
		{"[abc]", "d", false},
		{"[a-z]", "m", true}, // 范围
		{"[a-z]", "M", false},
		{"[!a-z]", "M", true}, // 否定集合
		{"[!a-z]", "m", false},
		{"file.[jt]s", "file.js", true},
		{"file.[jt]s", "file.ts", true},
		{"file.[jt]s", "file.go", false},
		{`\*`, "*", true}, // 转义通配字符按字面
		{`\*`, "ab", false},
		{`\?x`, "?x", true},
		{"[", "[", true}, // 未闭合的 [ 按字面
		{"a**b", "axxb", true},
		{"a**b", "axb", true}, // 连续星号按单星处理
	}
	for _, c := range cases {
		if got := matchSegment(seg(t, c.pat), c.name); got != c.want {
			t.Errorf("matchSegment(%q, %q) = %v, want %v", c.pat, c.name, got, c.want)
		}
	}
}

func match(t *testing.T, pat string, segs []string, isDir bool) bool {
	t.Helper()
	p := parseLine(pat, 1)
	if p == nil {
		t.Fatalf("%q 解析失败", pat)
	}
	return p.matchPath(segs, isDir)
}

func TestMatchPathDoubleStar(t *testing.T) {
	// 中间双星匹配零层目录。
	if !match(t, "a/**/b", []string{"a", "b"}, false) {
		t.Error("a/**/b 应匹配 a/b（零层）")
	}
	if !match(t, "a/**/b", []string{"a", "x", "y", "b"}, false) {
		t.Error("a/**/b 应匹配多层")
	}
	// 开头双星匹配零层目录。
	if !match(t, "**/gen", []string{"gen"}, true) {
		t.Error("**/gen 应匹配 gen（零层）")
	}
	// 末尾双星匹配目录内所有层级，但不匹配目录本身。
	if !match(t, "logs/**", []string{"logs", "a", "b"}, false) {
		t.Error("logs/** 应匹配 logs/a/b")
	}
	if match(t, "logs/**", []string{"logs"}, true) {
		t.Error("logs/** 不应匹配 logs 本身")
	}
}

func TestMatchPathAnchoredAndBasename(t *testing.T) {
	// 不含斜杠的规则匹配任意深度的名字。
	if !match(t, "*.py", []string{"a", "b", "x.py"}, false) {
		t.Error("*.py 应匹配任意深度")
	}
	// 锚定规则相对所在目录匹配。
	if match(t, "/a.py", []string{"sub", "a.py"}, false) {
		t.Error("/a.py 不应匹配子目录中的文件")
	}
	if !match(t, "doc/*.md", []string{"doc", "x.md"}, false) {
		t.Error("doc/*.md 应匹配 doc/x.md")
	}
	if match(t, "doc/*.md", []string{"doc", "sub", "x.md"}, false) {
		t.Error("doc/*.md 不应跨层匹配")
	}
	// 目录限定规则不匹配文件。
	if match(t, "build/", []string{"build"}, false) {
		t.Error("build/ 不应匹配同名文件")
	}
	if !match(t, "build/", []string{"build"}, true) {
		t.Error("build/ 应匹配目录")
	}
}
