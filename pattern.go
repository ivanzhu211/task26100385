package langstat

// token 是规则中的一个字符；esc 表示由反斜杠转义，按字面处理。
type token struct {
	ch  byte
	esc bool
}

// segment 是规则按斜杠切分后的一个路径段。
type segment struct {
	toks       []token // doubleStar 为 true 时为空
	doubleStar bool    // 完整的 ** 路径段
}

// pattern 是一条解析后的 .gitignore 规则。
type pattern struct {
	raw      string    // 规则原文（去掉行尾空格后）
	negate   bool      // 以未转义 ! 开头，表示重新纳入
	dirOnly  bool      // 以 / 结尾，只匹配目录
	anchored bool      // 含斜杠，相对所在 .gitignore 目录匹配
	segs     []segment // 按斜杠切分后的路径段
	line     int       // 在 .gitignore 中的行号（1 起）
}

// parseLine 解析一行 .gitignore 内容；空行与注释行返回 nil。
// 输入不含换行符；CRLF 的 \r 由 parseGitignore 先行剥离。
func parseLine(line string, lineNo int) *pattern {
	// 解析反斜杠转义，得到带转义标记的字符序列。
	toks := make([]token, 0, len(line))
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) {
			toks = append(toks, token{ch: line[i+1], esc: true})
			i++
			continue
		}
		toks = append(toks, token{ch: line[i]})
	}
	// 丢弃未转义的行尾空格，行首空格保留。
	for len(toks) > 0 && toks[len(toks)-1].ch == ' ' && !toks[len(toks)-1].esc {
		toks = toks[:len(toks)-1]
	}
	if len(toks) == 0 {
		return nil
	}
	// 未转义井号开头为注释。
	if toks[0].ch == '#' && !toks[0].esc {
		return nil
	}
	p := &pattern{raw: line, line: lineNo}
	// 未转义的前导感叹号表示重新纳入。
	if toks[0].ch == '!' && !toks[0].esc {
		p.negate = true
		toks = toks[1:]
		if len(toks) == 0 {
			return nil
		}
	}
	// 末尾斜杠表示只匹配目录。
	if toks[len(toks)-1].ch == '/' && !toks[len(toks)-1].esc {
		p.dirOnly = true
		toks = toks[:len(toks)-1]
		if len(toks) == 0 {
			return nil
		}
	}
	// 开头斜杠表示从所在目录起匹配，去掉后按含斜杠规则处理。
	leadingSlash := false
	if toks[0].ch == '/' && !toks[0].esc {
		leadingSlash = true
		toks = toks[1:]
		if len(toks) == 0 {
			return nil
		}
	}
	// 按斜杠切分路径段；切分前含斜杠（含已去掉的开头斜杠）的规则为锚定规则。
	hasSlash := p.dirOnly || leadingSlash
	cur := make([]token, 0, len(toks))
	flush := func() {
		if isDoubleStar(cur) {
			p.segs = append(p.segs, segment{doubleStar: true})
		} else if cur = collapseStars(cur); len(cur) > 0 {
			p.segs = append(p.segs, segment{toks: cur})
		}
		cur = make([]token, 0, len(toks))
	}
	for _, t := range toks {
		if t.ch == '/' && !t.esc {
			hasSlash = true
			flush()
			continue
		}
		cur = append(cur, t)
	}
	flush()
	p.anchored = hasSlash
	if len(p.segs) == 0 {
		return nil
	}
	return p
}

// collapseStars 将段内连续未转义星号压缩为单个星号。
// 完整的 ** 段在此之后由 isDoubleStar 识别，其余位置的连续星号按普通单星处理。
func collapseStars(toks []token) []token {
	out := toks[:0]
	star := false
	for _, t := range toks {
		if t.ch == '*' && !t.esc {
			if star {
				continue
			}
			star = true
		} else {
			star = false
		}
		out = append(out, t)
	}
	return out
}

// isDoubleStar 判断段是否为完整的双星（两个未转义星号），
// 必须在 collapseStars 之前调用。
func isDoubleStar(toks []token) bool {
	return len(toks) == 2 &&
		toks[0].ch == '*' && !toks[0].esc &&
		toks[1].ch == '*' && !toks[1].esc
}

// parseGitignore 解析整份 .gitignore 内容，支持 LF 与 CRLF。
func parseGitignore(data []byte) []pattern {
	var rules []pattern
	lineNo := 0
	start := 0
	for i := 0; i <= len(data); i++ {
		if i < len(data) && data[i] != '\n' {
			continue
		}
		lineNo++
		line := data[start:i]
		start = i + 1
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
		if p := parseLine(string(line), lineNo); p != nil {
			rules = append(rules, *p)
		}
	}
	return rules
}
