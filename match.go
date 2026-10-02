package langstat

// matchPath 判断规则 p 是否匹配相对路径 segs（相对所在 .gitignore 目录，
// isDir 表示目标是否为目录）。
func (p *pattern) matchPath(segs []string, isDir bool) bool {
	if p.dirOnly && !isDir {
		return false
	}
	if p.anchored {
		return matchSegs(p.segs, segs)
	}
	// 不含斜杠的规则匹配任意深度的文件名或目录名。
	if len(segs) == 0 {
		return false
	}
	return matchSegment(p.segs[0].toks, segs[len(segs)-1])
}

// matchSegs 将规则段序列与路径段序列对齐匹配。
// 双星段匹配零层或多层目录；末尾双星要求至少还剩一段，
// 即匹配目录内所有层级的条目但不匹配该目录本身。
func matchSegs(psegs []segment, path []string) bool {
	if len(psegs) == 0 {
		return len(path) == 0
	}
	if psegs[0].doubleStar {
		if len(psegs) == 1 {
			return len(path) >= 1
		}
		for i := 0; i <= len(path); i++ {
			if matchSegs(psegs[1:], path[i:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 {
		return false
	}
	if !matchSegment(psegs[0].toks, path[0]) {
		return false
	}
	return matchSegs(psegs[1:], path[1:])
}

// matchSegment 在单个路径段内匹配通配（* ? [...]），不跨越斜杠。
func matchSegment(pat []token, name string) bool {
	px, nx := 0, 0
	starPx, starNx := -1, -1
	for nx < len(name) {
		if px < len(pat) {
			t := pat[px]
			switch {
			case t.ch == '*' && !t.esc:
				starPx, starNx = px, nx
				px++
				continue
			case t.ch == '[' && !t.esc:
				if ok, next := matchClass(pat, px, name[nx]); ok {
					px, nx = next, nx+1
					continue
				}
			case t.ch == '?' && !t.esc:
				px, nx = px+1, nx+1
				continue
			case t.ch == name[nx]:
				px, nx = px+1, nx+1
				continue
			}
		}
		if starPx >= 0 {
			starNx++
			nx = starNx
			px = starPx + 1
			continue
		}
		return false
	}
	for px < len(pat) && pat[px].ch == '*' && !pat[px].esc {
		px++
	}
	return px == len(pat)
}

// matchClass 匹配从 pat[i]（[ ）开始的字符集合，返回是否命中及集合结束后的下标。
// 支持范围（a-z）与以感叹号开头的否定集合；未闭合的 [ 按字面处理。
func matchClass(pat []token, i int, ch byte) (bool, int) {
	j := i + 1
	negate := false
	if j < len(pat) && pat[j].ch == '!' && !pat[j].esc {
		negate = true
		j++
	}
	matched := false
	first := true
	for j < len(pat) {
		t := pat[j]
		if t.ch == ']' && !t.esc && !first {
			if negate {
				matched = !matched
			}
			return matched, j + 1
		}
		first = false
		lo := t.ch
		// 范围：a-z，连字符两侧都不是集合结尾。
		if j+2 < len(pat) && pat[j+1].ch == '-' && !pat[j+1].esc &&
			!(pat[j+2].ch == ']' && !pat[j+2].esc) {
			hi := pat[j+2].ch
			if lo <= ch && ch <= hi {
				matched = true
			}
			j += 3
			continue
		}
		if lo == ch {
			matched = true
		}
		j++
	}
	// 未闭合的 [ 按字面字符处理。
	return ch == '[', i + 1
}
