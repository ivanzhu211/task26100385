package langstat

import "strings"

// pattern 表示一条已解析的 gitignore 规则。
type pattern struct {
	raw      string // 规则原文（去除行尾空白、未去转义）
	negate   bool   // 以未转义 ! 开头，表示重新纳入
	dirOnly  bool   // 以 / 结尾，仅匹配目录
	anchored bool   // 去掉末尾 / 后仍含 /，相对于 .gitignore 所在目录匹配
	segments []string
}

// parsePattern 将一行 gitignore 文本解析为规则；不产生规则时返回 nil。
// 假定行已去掉换行符；此处处理 CRLF 的 \r。
func parsePattern(line string) *pattern {
	line = strings.TrimSuffix(line, "\r")
	// 丢弃未转义的行尾空格。
	for len(line) > 0 && line[len(line)-1] == ' ' && !escapedAt(line, len(line)-1) {
		line = line[:len(line)-1]
	}
	if line == "" || line[0] == '#' {
		return nil
	}
	p := &pattern{raw: line}
	s := line
	if s[0] == '!' {
		p.negate = true
		s = s[1:]
		if s == "" {
			return nil
		}
	}
	// 末尾斜杠：目录标记。转义的 \/ 不属于本工具需要关心的输入（规则语法有效）。
	if strings.HasSuffix(s, "/") {
		p.dirOnly = true
		s = strings.TrimRight(s, "/")
		if s == "" {
			return nil
		}
	}
	if strings.HasPrefix(s, "/") {
		p.anchored = true
		s = s[1:]
	}
	if strings.Contains(s, "/") {
		p.anchored = true
	}
	p.segments = strings.Split(s, "/")
	return p
}

// escapedAt 判断 line[i] 是否被奇数个连续反斜杠转义。
func escapedAt(line string, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && line[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

// match 判断规则是否匹配相对路径段序列（相对于 .gitignore 所在目录）。
func (p *pattern) match(segs []string, isDir bool) bool {
	if p.dirOnly && !isDir {
		return false
	}
	if len(segs) == 0 {
		return false
	}
	if !p.anchored {
		// 不含斜杠：匹配任意深度的文件名或目录名，即路径最后一段。
		return globMatch(p.segments[0], segs[len(segs)-1])
	}
	return matchSegments(p.segments, segs)
}

// matchSegments 匹配锚定规则的段序列。
func matchSegments(pat, path []string) bool {
	if len(pat) == 0 {
		return len(path) == 0
	}
	if pat[0] == "**" {
		if len(pat) == 1 {
			// 末尾 /**：匹配目录内所有层级条目，但不匹配目录本身。
			return len(path) >= 1
		}
		// 开头或中间的完整 ** 段：匹配零层或多层目录。
		for skip := 0; skip <= len(path); skip++ {
			if matchSegments(pat[1:], path[skip:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 || !globMatch(pat[0], path[0]) {
		return false
	}
	return matchSegments(pat[1:], path[1:])
}

// globMatch 在单个路径段内匹配 glob 模式：* ? [...] 与反斜杠转义。
// 段内不含斜杠，因此所有通配天然不跨斜杠。
func globMatch(pat, name string) bool {
	return globAt(pat, 0, name, 0)
}

func globAt(pat string, pi int, name string, ni int) bool {
	for pi < len(pat) {
		c := pat[pi]
		switch c {
		case '*':
			// 连续星号按普通单星处理，折叠为一个。
			for pi < len(pat) && pat[pi] == '*' {
				pi++
			}
			if pi == len(pat) {
				return true
			}
			for k := ni; k <= len(name); k++ {
				if globAt(pat, pi, name, k) {
					return true
				}
			}
			return false
		case '?':
			if ni >= len(name) {
				return false
			}
			pi++
			ni++
		case '[':
			if ni >= len(name) {
				return false
			}
			ok, next := matchClass(pat, pi, name[ni])
			if !ok {
				return false
			}
			pi = next
			ni++
		case '\\':
			if pi+1 >= len(pat) {
				// 末尾孤立反斜杠：按字面反斜杠处理（输入语法有效，防御性分支）。
				if ni >= len(name) || name[ni] != '\\' {
					return false
				}
				pi++
				ni++
				continue
			}
			if ni >= len(name) || name[ni] != pat[pi+1] {
				return false
			}
			pi += 2
			ni++
		default:
			if ni >= len(name) || name[ni] != c {
				return false
			}
			pi++
			ni++
		}
	}
	return ni == len(name)
}

// matchClass 匹配 pat[pi] 开始的方括号字符集合，返回是否命中及集合后的下标。
func matchClass(pat string, pi int, ch byte) (bool, int) {
	i := pi + 1
	negate := false
	if i < len(pat) && pat[i] == '!' {
		negate = true
		i++
	}
	matched := false
	first := true
	for i < len(pat) && (pat[i] != ']' || first) {
		first = false
		var lo byte
		if pat[i] == '\\' && i+1 < len(pat) {
			lo = pat[i+1]
			i += 2
		} else {
			lo = pat[i]
			i++
		}
		// 范围 a-z
		if i+1 < len(pat) && pat[i] == '-' && pat[i+1] != ']' {
			hi := pat[i+1]
			i += 2
			if lo <= ch && ch <= hi {
				matched = true
			}
		} else if lo == ch {
			matched = true
		}
	}
	if i < len(pat) {
		i++ // 跳过 ']'
	}
	if negate {
		matched = !matched
	}
	return matched, i
}
