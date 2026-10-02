package langstat

import "testing"

func TestParsePatternBasics(t *testing.T) {
	cases := []struct {
		line     string
		wantNil  bool
		negate   bool
		dirOnly  bool
		anchored bool
	}{
		{"", true, false, false, false},
		{"   ", true, false, false, false}, // 未转义行尾空格丢弃后为空
		{"# comment", true, false, false, false},
		{"\\#not-comment", false, false, false, false},
		{"!keep.py", false, true, false, false},
		{"\\!literal.py", false, false, false, false},
		{"build/", false, false, true, false},
		{"doc/*.txt", false, false, false, true},
		{"/root-only.py", false, false, false, true},
		{"*.py", false, false, false, false},
		{"foo\\ bar.py", false, false, false, false},
		{"trail.py   ", false, false, false, false},
		{"keep.py\\ ", false, false, false, false}, // 转义空格保留
	}
	for _, c := range cases {
		p := parsePattern(c.line)
		if c.wantNil {
			if p != nil {
				t.Errorf("%q: 期望无规则，得到 %+v", c.line, p)
			}
			continue
		}
		if p == nil {
			t.Errorf("%q: 期望有规则", c.line)
			continue
		}
		if p.negate != c.negate || p.dirOnly != c.dirOnly || p.anchored != c.anchored {
			t.Errorf("%q: negate=%v dirOnly=%v anchored=%v，期望 %v/%v/%v",
				c.line, p.negate, p.dirOnly, p.anchored, c.negate, c.dirOnly, c.anchored)
		}
	}
}

func TestParsePatternCRLF(t *testing.T) {
	p := parsePattern("*.py\r")
	if p == nil || p.segments[0] != "*.py" {
		t.Fatalf("CRLF 未正确处理: %+v", p)
	}
	if parsePattern("# c\r") != nil {
		t.Fatal("CRLF 注释行不应产生规则")
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pat, name string
		want      bool
	}{
		{"*.py", "a.py", true},
		{"*.py", "a.js", false},
		{"*", "anything", true},
		{"***", "anything", true}, // 连续星号按单星处理
		{"a**b", "ab", true},      // 段内连续星号按单星处理
		{"a**b", "axb", true},
		{"a?c", "abc", true},
		{"a?c", "ac", false},
		{"[abc]x", "bx", true},
		{"[abc]x", "dx", false},
		{"[a-c]x", "bx", true},
		{"[!abc]x", "dx", true},
		{"[!abc]x", "ax", false},
		{"[!a-c]x", "dx", true},
		{"\\*.py", "*.py", true},
		{"\\*.py", "a.py", false},
		{"foo\\ bar", "foo bar", true},
		{"\\#file", "#file", true},
	}
	for _, c := range cases {
		if got := globMatch(c.pat, c.name); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v，期望 %v", c.pat, c.name, got, c.want)
		}
	}
}

func TestPatternMatch(t *testing.T) {
	mk := func(line string) *pattern { return parsePattern(line) }
	cases := []struct {
		line  string
		segs  []string
		isDir bool
		want  bool
	}{
		{"build", []string{"build"}, true, true},
		{"build", []string{"src", "build"}, true, true},  // 任意深度目录名
		{"build", []string{"src", "build"}, false, true}, // 任意深度文件名
		{"build/", []string{"build"}, true, true},
		{"build/", []string{"build"}, false, false}, // 仅目录
		{"/a/b.py", []string{"a", "b.py"}, false, true},
		{"/a/b.py", []string{"x", "a", "b.py"}, false, false}, // 锚定根
		{"doc/*.txt", []string{"doc", "f.txt"}, false, true},
		{"doc/*.txt", []string{"doc", "sub", "f.txt"}, false, false},
		{"doc/**/*.txt", []string{"doc", "f.txt"}, false, true}, // ** 零层
		{"doc/**/*.txt", []string{"doc", "a", "b", "f.txt"}, false, true},
		{"**/gen/*.py", []string{"gen", "a.py"}, false, true}, // 开头 ** 零层
		{"**/gen/*.py", []string{"x", "y", "gen", "a.py"}, false, true},
		{"foo/**", []string{"foo"}, true, false}, // 不匹配目录本身
		{"foo/**", []string{"foo", "a"}, false, true},
		{"foo/**", []string{"foo", "a", "b", "c.py"}, false, true},
		{"a/*/c", []string{"a", "b", "c"}, false, true},
		{"a/*/c", []string{"a", "b", "d", "c"}, false, false},
	}
	for _, c := range cases {
		p := mk(c.line)
		if p == nil {
			t.Fatalf("%q 解析失败", c.line)
		}
		if got := p.match(c.segs, c.isDir); got != c.want {
			t.Errorf("%q match(%v, dir=%v) = %v，期望 %v", c.line, c.segs, c.isDir, got, c.want)
		}
	}
}
