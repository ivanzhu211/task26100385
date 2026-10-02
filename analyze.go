package langstat

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LanguageStat 记录单语言统计。
type LanguageStat struct {
	Language string
	Bytes    int64
	Ratio    float64 // 占全部纳入源码字节的比例
}

// Report 为一次完整统计的结果。
type Report struct {
	Languages  []LanguageStat // 按字节数降序
	TotalBytes int64
	Files      int // 纳入统计的文件数
}

// Explanation 解释某个文件为何被计入或排除。
type Explanation struct {
	Path     string
	Included bool
	Counted  bool   // 是否实际计入语言统计（受扩展名限制）
	Default  bool   // 无规则命中，默认纳入
	RuleFile string // 决定结果的 .gitignore 路径（相对根目录；Default 时为空）
	RuleLine int    // 决定结果的行号（Default 时为 0）
	RuleText string // 规则原文
}

// Analyzer 持有对源码目录的分析结果，只读。
type Analyzer struct {
	root   string
	byLang map[string]int64
	files  int
	total  int64
}

var langByExt = map[string]string{
	".py": "Python",
	".js": "JavaScript",
	".ts": "TypeScript",
	".go": "Go",
}

// Analyze 遍历 root，按嵌套 .gitignore 规则统计各语言字节量。
func Analyze(root string) (*Analyzer, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("langstat: %s 不是目录", root)
	}
	a := &Analyzer{root: root, byLang: make(map[string]int64)}
	rs := &ruleSet{}
	if err := a.walkDir(nil, rs); err != nil {
		return nil, err
	}
	return a, nil
}

// walkDir 处理 dirSegs 指向的目录；进入前该目录已被判定为未排除。
func (a *Analyzer) walkDir(dirSegs []string, rs *ruleSet) error {
	abs := a.root
	if len(dirSegs) > 0 {
		abs = filepath.Join(append([]string{a.root}, dirSegs...)...)
	}
	// 只要目录未被排除，就读取其中的 .gitignore，即使该文件本身会被忽略。
	relFile := strings.Join(append(append([]string{}, dirSegs...), ".gitignore"), "/")
	group, err := loadIgnoreFile(filepath.Join(abs, ".gitignore"), dirSegs, relFile)
	if err == nil {
		rs.groups = append(rs.groups, group)
		defer func() { rs.groups = rs.groups[:len(rs.groups)-1] }()
	} else if !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return err
	}
	for _, e := range entries {
		segs := append(append([]string{}, dirSegs...), e.Name())
		if e.IsDir() {
			// 被排除的目录整个子树不可纳入，其内部 .gitignore 不参与判断。
			if _, excluded := rs.decide(segs, true); excluded {
				continue
			}
			if err := a.walkDir(segs, rs); err != nil {
				return err
			}
			continue
		}
		if !e.Type().IsRegular() {
			continue
		}
		lang, ok := langByExt[filepath.Ext(e.Name())]
		if !ok {
			continue
		}
		if _, excluded := rs.decide(segs, false); excluded {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		size := info.Size()
		a.byLang[lang] += size
		a.total += size
		a.files++
	}
	return nil
}

// Report 返回语言统计报告。
func (a *Analyzer) Report() *Report {
	rep := &Report{TotalBytes: a.total, Files: a.files}
	for lang, bytes := range a.byLang {
		st := LanguageStat{Language: lang, Bytes: bytes}
		if a.total > 0 {
			st.Ratio = float64(bytes) / float64(a.total)
		}
		rep.Languages = append(rep.Languages, st)
	}
	sort.Slice(rep.Languages, func(i, j int) bool {
		if rep.Languages[i].Bytes != rep.Languages[j].Bytes {
			return rep.Languages[i].Bytes > rep.Languages[j].Bytes
		}
		return rep.Languages[i].Language < rep.Languages[j].Language
	})
	return rep
}

// Explain 解释相对路径 path（斜杠分隔）对应的现存文件是否纳入统计。
// 若被祖先目录屏蔽，指向该祖先的排除规则。
func (a *Analyzer) Explain(path string) (*Explanation, error) {
	segs := splitPath(path)
	if len(segs) == 0 {
		return nil, fmt.Errorf("langstat: 路径为空")
	}
	abs := filepath.Join(append([]string{a.root}, segs...)...)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("langstat: %s 是目录，Explain 仅支持文件", path)
	}
	exp := &Explanation{Path: path, Included: true}
	_, counted := langByExt[filepath.Ext(segs[len(segs)-1])]
	exp.Counted = counted

	rs := &ruleSet{}
	// 逐级下潜：先加载当前目录的 .gitignore，再判定下一级路径段。
	for i := 0; i < len(segs); i++ {
		dirSegs := segs[:i]
		absDir := a.root
		if len(dirSegs) > 0 {
			absDir = filepath.Join(append([]string{a.root}, dirSegs...)...)
		}
		relFile := strings.Join(append(append([]string{}, dirSegs...), ".gitignore"), "/")
		if group, err := loadIgnoreFile(filepath.Join(absDir, ".gitignore"), dirSegs, relFile); err == nil {
			rs.groups = append(rs.groups, group)
		}
		isLast := i == len(segs)-1
		r, excluded := rs.decide(segs[:i+1], !isLast)
		if excluded {
			// 目录被排除时其全部后代被屏蔽，解释指向该祖先规则。
			exp.Included = false
			exp.Counted = false
			exp.RuleFile = r.file
			exp.RuleLine = r.line
			exp.RuleText = r.pat.raw
			return exp, nil
		}
		if isLast && r != nil {
			// 命中的最后一条规则为反选（重新纳入）。
			exp.RuleFile = r.file
			exp.RuleLine = r.line
			exp.RuleText = r.pat.raw
			return exp, nil
		}
	}
	exp.Default = true
	return exp, nil
}
