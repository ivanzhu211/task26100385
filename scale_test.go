package langstat

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// maxRSS 返回进程峰值内存（字节）。
func maxRSS() int64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return ru.Maxrss // darwin 与 linux 均为字节
}

// TestScale 在 10 万文件、2000 目录、1000 份 .gitignore（每份 40 行）、
// 深度 20 层的输入上验证：完整统计 < 10 秒，进程峰值内存 < 128 MiB。
// 耗时不包含输入目录的准备时间。
func TestScale(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过规模测试")
	}
	root := t.TempDir()

	const (
		topDirs    = 20
		midDirs    = 10
		leafDirs   = 10
		filesPer   = 50
		ruleLines  = 40
		deepLevels = 20
	)
	exts := []string{".py", ".js", ".ts", ".go"}
	content := []byte("package x // 1234567890\n")

	// 构造 40 行规则：混合锚定、双星、否定、目录限定与字符集。
	ruleText := make([]byte, 0, 512)
	for i := 0; i < ruleLines; i++ {
		switch i % 5 {
		case 0:
			fmt.Fprintf((*ruleBuilder)(&ruleText), "tmp%d/\n", i)
		case 1:
			fmt.Fprintf((*ruleBuilder)(&ruleText), "**/cache%d/**\n", i)
		case 2:
			fmt.Fprintf((*ruleBuilder)(&ruleText), "!keep%d.go\n", i)
		case 3:
			fmt.Fprintf((*ruleBuilder)(&ruleText), "gen_[a-z]%d.py\n", i)
		default:
			fmt.Fprintf((*ruleBuilder)(&ruleText), "*.log%d\n", i)
		}
	}

	// 准备目录树（不计入耗时）：20×10×10 = 2000 目录，每目录 50 文件。
	fileCount := 0
	gitignoreCount := 0
	var dirs []string
	for a := 0; a < topDirs; a++ {
		for b := 0; b < midDirs; b++ {
			for c := 0; c < leafDirs; c++ {
				dirs = append(dirs, fmt.Sprintf("t%02d/m%02d/l%02d", a, b, c))
			}
		}
	}
	// 附加一条 20 层深的目录链。
	deep := "deep"
	for i := 0; i < deepLevels; i++ {
		deep += "/d"
		dirs = append(dirs, deep)
	}
	for i, rel := range dirs {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(abs, 0o755); err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 && gitignoreCount < 1000 {
			if err := os.WriteFile(filepath.Join(abs, ".gitignore"), ruleText, 0o644); err != nil {
				t.Fatal(err)
			}
			gitignoreCount++
		}
		for f := 0; f < filesPer; f++ {
			name := fmt.Sprintf("f%02d%s", f, exts[f%len(exts)])
			if err := os.WriteFile(filepath.Join(abs, name), content, 0o644); err != nil {
				t.Fatal(err)
			}
			fileCount++
		}
	}
	t.Logf("输入规模: %d 文件, %d 目录, %d 份 .gitignore", fileCount, len(dirs), gitignoreCount)

	// 计时仅覆盖统计本身。
	start := time.Now()
	rep, err := NewAnalyzer(root).Analyze()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	runtime.GC()
	rss := maxRSS()
	t.Logf("统计耗时 %v，进程峰值内存 %.1f MiB，纳入 %d 文件 / %d 字节",
		elapsed, float64(rss)/(1<<20), rep.TotalFiles, rep.TotalBytes)

	if elapsed > 10*time.Second {
		t.Errorf("统计耗时 %v 超过 10 秒", elapsed)
	}
	if limit := int64(128 << 20); rss > limit {
		t.Errorf("进程峰值内存 %.1f MiB 超过 128 MiB", float64(rss)/(1<<20))
	}
	if rep.TotalFiles != fileCount {
		t.Errorf("TotalFiles = %d, want %d（规则不应命中任何生成文件）", rep.TotalFiles, fileCount)
	}
	wantBytes := int64(fileCount) * int64(len(content))
	if rep.TotalBytes != wantBytes {
		t.Errorf("TotalBytes = %d, want %d", rep.TotalBytes, wantBytes)
	}
}

type ruleBuilder []byte

func (b *ruleBuilder) Write(p []byte) (int, error) {
	*b = append(*b, p...)
	return len(p), nil
}
