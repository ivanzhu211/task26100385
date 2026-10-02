package langstat

import "testing"

func TestParseLineCommentAndBlank(t *testing.T) {
	for _, line := range []string{"", "   ", "# comment", "  # not-comment? "} {
		// "  # not-comment? " 行首空格保留，但去尾空格后以 # 开头吗？
		// 行首是空格而非 #，因此不是注释；这里只断言纯注释与空行。
		if line == "  # not-comment? " {
			continue
		}
		if p := parseLine(line, 1); p != nil {
			t.Fatalf("parseLine(%q) = %+v, want nil", line, p)
		}
	}
	// 行首空格保留：以空格开头不是注释。
	if p := parseLine(" #x", 1); p == nil {
		t.Fatal("以空格开头的 # 不应视为注释")
	}
	// 转义井号不是注释。
	p := parseLine(`\#hash`, 1)
	if p == nil || p.negate {
		t.Fatalf(`\#hash 解析错误: %+v`, p)
	}
}

func TestParseLineTrailingSpace(t *testing.T) {
	p := parseLine("foo   ", 1)
	if p == nil || len(p.segs) != 1 || string(tokensBytes(p.segs[0].toks)) != "foo" {
		t.Fatalf("未转义行尾空格应丢弃: %+v", p)
	}
	p = parseLine(`foo\ `, 1)
	if p == nil || string(tokensBytes(p.segs[0].toks)) != "foo " {
		t.Fatalf("转义行尾空格应保留: %+v", p)
	}
}

func TestParseLineNegation(t *testing.T) {
	p := parseLine("!keep.go", 1)
	if p == nil || !p.negate {
		t.Fatalf("!keep.go 应为反选: %+v", p)
	}
	p = parseLine(`\!literal`, 1)
	if p == nil || p.negate {
		t.Fatalf(`\!literal 不应为反选: %+v`, p)
	}
}

func TestParseLineDirOnlyAndAnchored(t *testing.T) {
	p := parseLine("build/", 1)
	if p == nil || !p.dirOnly || !p.anchored {
		t.Fatalf("build/ 应为目录限定且锚定: %+v", p)
	}
	p = parseLine("/root.go", 1)
	if p == nil || p.dirOnly || !p.anchored {
		t.Fatalf("/root.go 应锚定: %+v", p)
	}
	p = parseLine("a/b.go", 1)
	if p == nil || !p.anchored || len(p.segs) != 2 {
		t.Fatalf("a/b.go 应锚定且两段: %+v", p)
	}
	p = parseLine("*.go", 1)
	if p == nil || p.anchored || p.dirOnly {
		t.Fatalf("*.go 不应锚定: %+v", p)
	}
}

func TestParseLineDoubleStar(t *testing.T) {
	p := parseLine("a/**/b", 1)
	if p == nil || len(p.segs) != 3 || !p.segs[1].doubleStar {
		t.Fatalf("a/**/b 中段应为双星: %+v", p)
	}
	p = parseLine("logs/**", 1)
	if p == nil || len(p.segs) != 2 || !p.segs[1].doubleStar {
		t.Fatalf("logs/** 末段应为双星: %+v", p)
	}
	p = parseLine("**/gen", 1)
	if p == nil || len(p.segs) != 2 || !p.segs[0].doubleStar {
		t.Fatalf("**/gen 首段应为双星: %+v", p)
	}
	// 其他位置的连续星号按普通单星处理。
	p = parseLine("a**b", 1)
	if p == nil || len(p.segs) != 1 || p.segs[0].doubleStar {
		t.Fatalf("a**b 不应为双星段: %+v", p)
	}
	if got := string(tokensBytes(p.segs[0].toks)); got != "a*b" {
		t.Fatalf("a**b 应压缩为 a*b，得到 %q", got)
	}
	p = parseLine("***", 1)
	if p == nil || p.segs[0].doubleStar || string(tokensBytes(p.segs[0].toks)) != "*" {
		t.Fatalf("*** 应压缩为单星: %+v", p)
	}
}

func TestParseGitignoreCRLF(t *testing.T) {
	rules := parseGitignore([]byte("foo\r\nbar\n# c\r\n\r\nbaz/"))
	if len(rules) != 3 {
		t.Fatalf("CRLF 混用应解析出 3 条规则，得到 %d", len(rules))
	}
	if rules[0].line != 1 || rules[1].line != 2 || rules[2].line != 5 {
		t.Fatalf("行号错误: %d %d %d", rules[0].line, rules[1].line, rules[2].line)
	}
	if !rules[2].dirOnly {
		t.Fatal("baz/ 应为目录限定")
	}
}

// tokensBytes 提取段的字符内容（忽略转义标记），仅用于测试断言。
func tokensBytes(toks []token) []byte {
	b := make([]byte, len(toks))
	for i, tk := range toks {
		b[i] = tk.ch
	}
	return b
}
