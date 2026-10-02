package langstat

import (
	"bufio"
	"os"
	"strings"
)

// rule 携带规则来源信息，用于解释。
type rule struct {
	pat  *pattern
	file string // .gitignore 文件路径（相对根目录，斜杠分隔）
	line int    // 1 起始行号
}

// ruleGroup 是同一份 .gitignore 解析出的规则集合。
type ruleGroup struct {
	dirSegs []string // .gitignore 所在目录相对根目录的段序列（根为空）
	rules   []rule
}

// ruleSet 保存分析过程中某一目录链上生效的全部规则组，
// 顺序为祖先在前、较深目录在后。
type ruleSet struct {
	groups []ruleGroup
}

// loadIgnoreFile 解析一份 .gitignore，返回其中的规则（保持行顺序）。
func loadIgnoreFile(path string, dirSegs []string, relFile string) (ruleGroup, error) {
	g := ruleGroup{dirSegs: dirSegs}
	f, err := os.Open(path)
	if err != nil {
		return g, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if p := parsePattern(sc.Text()); p != nil {
			g.rules = append(g.rules, rule{pat: p, file: relFile, line: lineNo})
		}
	}
	return g, sc.Err()
}

// decide 判断路径（相对根目录的段序列）是否被排除。
// 返回决定结果的规则（无匹配时为 nil）与是否排除。
// 应用顺序：祖先规则先应用，同文件内后面的规则覆盖前面的规则。
func (rs *ruleSet) decide(segs []string, isDir bool) (*rule, bool) {
	var decided *rule
	excluded := false
	for gi := range rs.groups {
		g := &rs.groups[gi]
		if len(segs) <= len(g.dirSegs) || !hasPrefix(segs, g.dirSegs) {
			continue
		}
		rel := segs[len(g.dirSegs):]
		for ri := range g.rules {
			r := &g.rules[ri]
			if r.pat.match(rel, isDir) {
				decided = r
				excluded = !r.pat.negate
			}
		}
	}
	return decided, excluded
}

func hasPrefix(segs, prefix []string) bool {
	if len(segs) < len(prefix) {
		return false
	}
	for i, s := range prefix {
		if segs[i] != s {
			return false
		}
	}
	return true
}

// splitPath 将斜杠分隔的相对路径拆成段序列。
func splitPath(rel string) []string {
	if rel == "" || rel == "." {
		return nil
	}
	return strings.Split(strings.Trim(rel, "/"), "/")
}
