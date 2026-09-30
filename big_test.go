package tiffthumb_test

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"tiffthumb"
)

const (
	bigW = 10000
	bigH = 6000
)

// patternPixel 是确定性像素图案，避免在内存中保存 180MB 测试数据。
func patternPixel(x, y int) (uint8, uint8, uint8) {
	return uint8(x*7 + y*13), uint8(x/7 + y*3), uint8(x*x/1000 + y)
}

// streamBigTIFF 流式生成大图 TIFF，不在内存中保留整幅图像。
func streamBigTIFF(t *testing.T, path string, rps int, phys []int) {
	t.Helper()
	header, counts, _ := tiffLayout(false, bigW, bigH, 3, 1, rps, phys)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(header); err != nil {
		t.Fatal(err)
	}
	nStrips := len(counts)
	if phys == nil {
		phys = make([]int, nStrips)
		for i := range phys {
			phys[i] = i
		}
	}
	row := make([]byte, bigW*3)
	for _, logical := range phys {
		y0 := logical * rps
		rows := int(counts[logical]) / (bigW * 3)
		for y := y0; y < y0+rows; y++ {
			for x := 0; x < bigW; x++ {
				r, g, b := patternPixel(x, y)
				row[x*3+0] = r
				row[x*3+1] = g
				row[x*3+2] = b
			}
			if _, err := f.Write(row); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func maxRSSBytes() int64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	// macOS 报告字节，Linux 报告 KiB；本任务运行环境为 macOS。
	return ru.Maxrss
}

// TestBigImage 大图：整图单条带与物理乱序条带结果一致，并报告耗时与峰值内存。
func TestBigImage(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过大图测试")
	}
	dir := t.TempDir()

	// 布局 A：整图单条带。
	pathA := filepath.Join(dir, "single.tiff")
	streamBigTIFF(t, pathA, bigH, nil)

	// 布局 B：100 行一个条带，共 60 条，物理乱序。
	nStrips := bigH / 100
	phys := rand.New(rand.NewSource(7)).Perm(nStrips)
	pathB := filepath.Join(dir, "shuffled.tiff")
	streamBigTIFF(t, pathB, 100, phys)

	run := func(src, tag string) [][]byte {
		outs := make([]string, 3)
		for i, s := range tiffthumb.ThumbSizes {
			outs[i] = filepath.Join(dir, fmt.Sprintf("%s_%d.png", tag, s))
		}
		runtime.GC()
		rssBefore := maxRSSBytes()
		start := time.Now()
		if err := tiffthumb.GenerateThumbnails(src, outs); err != nil {
			t.Fatalf("%s: GenerateThumbnails 失败: %v", tag, err)
		}
		elapsed := time.Since(start)
		rssAfter := maxRSSBytes()
		t.Logf("%s: 三档合计耗时 %v, 峰值常驻内存增量 %.1f MiB",
			tag, elapsed, float64(rssAfter-rssBefore)/(1<<20))
		if elapsed > 30*time.Second {
			t.Errorf("%s: 耗时 %v 超过 30 秒预算", tag, elapsed)
		}
		if rssAfter-rssBefore > 96*(1<<20) {
			t.Errorf("%s: 峰值常驻内存增量 %.1f MiB 超过 96 MiB 预算",
				tag, float64(rssAfter-rssBefore)/(1<<20))
		}
		pngs := make([][]byte, 3)
		for i, p := range outs {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			pngs[i] = b
		}
		return pngs
	}

	pngA := run(pathA, "单条带")
	pngB := run(pathB, "乱序条带")
	for i, s := range tiffthumb.ThumbSizes {
		if !bytes.Equal(pngA[i], pngB[i]) {
			t.Errorf("尺寸 %d: 不同存储布局的 PNG 输出不一致", s)
		}
	}

	// 显示信息核对。
	info, err := tiffthumb.ReadInfo(pathA)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != bigW || info.Height != bigH || info.Category != tiffthumb.Landscape {
		t.Errorf("显示信息 = %+v, 期望 %dx%d 横向", info, bigW, bigH)
	}
}
