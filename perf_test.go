package langstat

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// 规模检查：10 万文件、2000 目录、1000 份 .gitignore（每份 20 行）、深度 ≤ 20。
// 完整统计须在 10 秒内完成。生成输入的耗时不计入。
func TestScale(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过规模检查")
	}
	root := t.TempDir()

	// 构造目录树：14 × 12 × 11 ≈ 2030 个目录，深度 3。
	var dirs []string
	for a := 0; a < 14; a++ {
		d1 := fmt.Sprintf("d%02d", a)
		dirs = append(dirs, d1)
		for b := 0; b < 12; b++ {
			d2 := d1 + "/" + fmt.Sprintf("d%02d", b)
			dirs = append(dirs, d2)
			for c := 0; c < 11; c++ {
				d3 := d2 + "/" + fmt.Sprintf("d%02d", c)
				dirs = append(dirs, d3)
			}
		}
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// 1000 份 .gitignore，每份 20 行，混合各类规则。
	ignoreBody := ""
	for i := 0; i < 5; i++ {
		ignoreBody += fmt.Sprintf("tmp%d/\n*.log%d\n!keep%d.py\n/gen%d/**\nbuild%d\n", i, i, i, i, i)
	}
	for i := 0; i < 1000; i++ {
		p := filepath.Join(root, filepath.FromSlash(dirs[i*2]), ".gitignore")
		if err := os.WriteFile(p, []byte(ignoreBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 10 万个源码文件，均匀散布，四种扩展名轮转。
	exts := []string{".py", ".js", ".ts", ".go"}
	payload := make([]byte, 64)
	for i := 0; i < 100000; i++ {
		d := dirs[i%len(dirs)]
		name := fmt.Sprintf("f%05d%s", i, exts[i%4])
		p := filepath.Join(root, filepath.FromSlash(d), name)
		if err := os.WriteFile(p, payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	a, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	rep := a.Report()
	if rep.Files != 100000 {
		t.Errorf("Files = %d，期望 100000", rep.Files)
	}
	t.Logf("Analyze 耗时 %v，堆内存增量 %.1f MiB，文件 %d，总字节 %d",
		elapsed, float64(after.HeapAlloc-before.HeapAlloc)/1048576, rep.Files, rep.TotalBytes)
	if elapsed > 10*time.Second {
		t.Errorf("统计耗时 %v 超过 10 秒", elapsed)
	}
	if after.HeapAlloc > 128<<20 {
		t.Errorf("堆内存 %.1f MiB 超过 128 MiB", float64(after.HeapAlloc)/1048576)
	}
}
