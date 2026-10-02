// Package langstat 提供只读的仓库语言统计能力。
// 调用方提供本地源码目录，库按照目录内嵌套的 .gitignore
// 规则确定分析范围，统计各语言源码字节占比，并支持解释
// 任一文件被计入或排除的原因。
package langstat

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// sourceExts 是纳入统计的源码扩展名集合。
var sourceExts = map[string]bool{
	".py": true,
	".js": true,
	".ts": true,
	".go": true,
}

// ReasonKind 表示解释结果的类别。
type ReasonKind int

const (
	// ReasonDefaultInclude 表示没有任何规则命中，按默认纳入。
	ReasonDefaultInclude ReasonKind = iota
	// ReasonMatchedRule 表示由某条规则直接决定（排除或重新纳入）。
	ReasonMatchedRule
	// ReasonAncestorExcluded 表示被祖先目录的排除规则屏蔽。
	ReasonAncestorExcluded
)

// LangStat 是单一语言的统计结果。
type LangStat struct {
	Files   int
	Bytes   int64
	Percent float64 // 占全部纳入源码字节的比例，0-100
}

// Report 是一次完整统计的结果。
type Report struct {
	TotalBytes int64
	TotalFiles int
	Languages  map[string]*LangStat // 以扩展名（如 ".go"）为键
}

// Decision 是对单个文件是否纳入的解释。
type Decision struct {
	Included bool
	Reason   ReasonKind
	RuleFile string // 决定结果的 .gitignore 路径（相对根目录）；无规则时为空
	Line     int    // 决定结果的规则行号（1 起）；无规则时为 0
	Pattern  string // 决定结果的规则原文；无规则时为空
}

// Analyzer 绑定一个源码根目录，可重复执行统计与解释。
type Analyzer struct {
	root string
}

// NewAnalyzer 创建一个绑定 root 目录的分析器。
func NewAnalyzer(root string) *Analyzer {
	return &Analyzer{root: root}
}

// Analyze 遍历目录，返回语言统计报告。
func (a *Analyzer) Analyze() (*Report, error) {
	info, err := os.Stat(a.root)
	if err != nil {
		return nil, fmt.Errorf("langstat: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("langstat: %s 不是目录", a.root)
	}
	rep := &Report{Languages: make(map[string]*LangStat)}
	st := &ruleStack{}
	if err := a.walkDir(a.root, nil, st, rep); err != nil {
		return nil, err
	}
	for _, ls := range rep.Languages {
		if rep.TotalBytes > 0 {
			ls.Percent = float64(ls.Bytes) * 100 / float64(rep.TotalBytes)
		}
	}
	return rep, nil
}

// Explain 解释 rel（相对根目录）指向的现存文件是否被纳入统计。
func (a *Analyzer) Explain(rel string) (*Decision, error) {
	rel = path.Clean(filepath.ToSlash(rel))
	if rel == "." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
		return nil, fmt.Errorf("langstat: 非法相对路径 %q", rel)
	}
	info, err := os.Stat(filepath.Join(a.root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("langstat: %w", err)
	}
	segs := strings.Split(rel, "/")
	st := &ruleStack{}
	return a.explainDir(a.root, nil, segs, info.IsDir(), st)
}

// walkDir 递归遍历未排除目录，累计语言统计。
// segs 是 abs 相对根目录的路径段，根目录为 nil。
func (a *Analyzer) walkDir(abs string, segs []string, st *ruleStack, rep *Report) error {
	pushFrame(st, abs, segs)
	defer func() { st.frames = st.frames[:len(st.frames)-1] }()

	entries, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Errorf("langstat: %w", err)
	}
	for _, e := range entries {
		child := append(segs, e.Name())
		if e.IsDir() {
			if p, _ := st.decide(child, true); !included(p) {
				continue // 被排除目录的全部后代不可纳入
			}
			if err := a.walkDir(filepath.Join(abs, e.Name()), child, st, rep); err != nil {
				return err
			}
			continue
		}
		if !e.Type().IsRegular() {
			continue
		}
		ext := path.Ext(e.Name())
		if !sourceExts[ext] {
			continue
		}
		if p, _ := st.decide(child, false); !included(p) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			return fmt.Errorf("langstat: %w", err)
		}
		ls := rep.Languages[ext]
		if ls == nil {
			ls = &LangStat{}
			rep.Languages[ext] = ls
		}
		ls.Files++
		ls.Bytes += fi.Size()
		rep.TotalFiles++
		rep.TotalBytes += fi.Size()
	}
	return nil
}

// explainDir 沿 targetSegs 逐层下钻，解释目标文件的纳入状态。
// segs 是 abs 相对根目录的路径段，根目录为 nil。
func (a *Analyzer) explainDir(abs string, segs, targetSegs []string, targetIsDir bool, st *ruleStack) (*Decision, error) {
	pushFrame(st, abs, segs)
	defer func() { st.frames = st.frames[:len(st.frames)-1] }()

	child := targetSegs[:len(segs)+1]
	isLast := len(child) == len(targetSegs)
	p, f := st.decide(child, !isLast || targetIsDir)
	if !included(p) {
		d := &Decision{Included: false, Pattern: p.raw, Line: p.line, RuleFile: ruleFileOf(f)}
		if isLast {
			d.Reason = ReasonMatchedRule
		} else {
			// 祖先目录被排除，目标被屏蔽；指向该祖先的排除规则。
			d.Reason = ReasonAncestorExcluded
		}
		return d, nil
	}
	if isLast {
		if p != nil {
			return &Decision{Included: true, Reason: ReasonMatchedRule, Pattern: p.raw, Line: p.line, RuleFile: ruleFileOf(f)}, nil
		}
		return &Decision{Included: true, Reason: ReasonDefaultInclude}, nil
	}
	return a.explainDir(filepath.Join(abs, child[len(child)-1]), child, targetSegs, targetIsDir, st)
}

// pushFrame 读取目录内的 .gitignore 并压入规则栈。
// 只要目录未被排除就读取其 .gitignore，即使该规则文件本身会被忽略。
func pushFrame(st *ruleStack, abs string, segs []string) {
	relDir := "."
	if len(segs) > 0 {
		relDir = strings.Join(segs, "/")
	}
	frame := ruleFrame{relDir: relDir, depth: len(segs)}
	if data, err := os.ReadFile(filepath.Join(abs, ".gitignore")); err == nil {
		frame.rules = parseGitignore(data)
	}
	st.frames = append(st.frames, frame)
}

// ruleFileOf 返回规则所在 .gitignore 相对根目录的路径。
func ruleFileOf(f *ruleFrame) string {
	if f.relDir == "." {
		return ".gitignore"
	}
	return f.relDir + "/.gitignore"
}
