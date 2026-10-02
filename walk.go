package langstat

// ruleFrame 是一个目录层级上的规则集合。
type ruleFrame struct {
	relDir string // 相对根目录的目录路径（斜杠分隔），根为 "."
	depth  int    // 目录深度，根为 0
	rules  []pattern
}

// ruleStack 维护从根到当前目录的规则栈。
// 祖先规则先应用，较深目录的规则后应用。
type ruleStack struct {
	frames []ruleFrame
}

// decide 判定路径 segs（相对根目录，isDir 表示目标是否为目录）的纳入状态，
// 返回决定结果的规则及其所在层；无规则命中时返回 nil。
// 判定顺序：由最深目录向最浅目录，同一文件内后面的规则覆盖前面的规则。
func (s *ruleStack) decide(segs []string, isDir bool) (*pattern, *ruleFrame) {
	for i := len(s.frames) - 1; i >= 0; i-- {
		f := &s.frames[i]
		if len(segs) <= f.depth {
			continue
		}
		rel := segs[f.depth:]
		for j := len(f.rules) - 1; j >= 0; j-- {
			if f.rules[j].matchPath(rel, isDir) {
				return &f.rules[j], f
			}
		}
	}
	return nil, nil
}

// included 将判定结果转换为是否纳入。
func included(p *pattern) bool {
	return p == nil || p.negate
}
