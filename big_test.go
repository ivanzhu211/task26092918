package tiffthumb

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	bigW = 10000
	bigH = 6000
)

// bigPixel 是大图的确定性像素图案（源坐标）。
func bigPixel(x, y int) (r, g, b, a uint8) {
	v := uint32(x)*2654435761 ^ uint32(y)*2246822519
	v ^= v >> 13
	return uint8(v), uint8(v >> 8), uint8((x + y) % 251), 255
}

// TestBigChild 是被父测试以子进程方式调起的辅助测试，用于隔离测量峰值内存。
func TestBigChild(t *testing.T) {
	if os.Getenv("TIFFTHUMB_BIG_CHILD") != "1" {
		t.Skip("helper process")
	}
	src := os.Getenv("TIFFTHUMB_BIG_SRC")
	outDir := os.Getenv("TIFFTHUMB_BIG_OUT")
	var paths [3]string
	for i, n := range Sizes {
		paths[i] = fmt.Sprintf("%s/thumb_%d.png", outDir, n)
	}
	start := time.Now()
	if err := GenerateThumbnails(src, paths); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}
	maxrss := int64(ru.Maxrss)
	if runtime.GOOS != "darwin" { // Linux 的 ru_maxrss 单位是 KiB
		maxrss *= 1024
	}
	fmt.Printf("RESULT elapsed_ms=%d maxrss_bytes=%d\n", elapsed.Milliseconds(), maxrss)
}

type bigResult struct {
	elapsed time.Duration
	maxRSS  int64
	pngs    [3][]byte
}

// runBigChild 在子进程中生成三档缩略图并回报耗时与峰值常驻内存。
func runBigChild(t *testing.T, src string) bigResult {
	t.Helper()
	outDir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestBigChild", "-test.v")
	cmd.Env = append(os.Environ(),
		"TIFFTHUMB_BIG_CHILD=1",
		"TIFFTHUMB_BIG_SRC="+src,
		"TIFFTHUMB_BIG_OUT="+outDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child failed: %v\n%s", err, out)
	}
	var res bigResult
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "RESULT ") {
			continue
		}
		var ms, rss int64
		if _, err := fmt.Sscanf(line, "RESULT elapsed_ms=%d maxrss_bytes=%d", &ms, &rss); err != nil {
			t.Fatalf("parse result line %q: %v", line, err)
		}
		res.elapsed = time.Duration(ms) * time.Millisecond
		res.maxRSS = rss
	}
	if res.elapsed == 0 {
		t.Fatalf("no RESULT line in child output:\n%s", out)
	}
	for i, n := range Sizes {
		data, err := os.ReadFile(fmt.Sprintf("%s/thumb_%d.png", outDir, n))
		if err != nil {
			t.Fatal(err)
		}
		res.pngs[i] = data
	}
	return res
}

// TestBigImage 10000×6000 RGB：整图单条带与物理乱序条带的结果必须一致，
// 并报告三档合计耗时与峰值常驻内存。
func TestBigImage(t *testing.T) {
	if testing.Short() {
		t.Skip("big image test skipped in -short mode")
	}
	dir := t.TempDir()
	mkSpec := func() tiffSpec {
		return tiffSpec{
			width: bigW, height: bigH, spp: 3, orientation: 1,
			pixel: func(x, y int) (r, g, b, a uint8) { return bigPixel(x, y) },
		}
	}
	// 布局 A：整图一个条带。
	specA := mkSpec()
	pathA := dir + "/single_strip.tiff"
	writeTIFF(t, pathA, specA)
	// 布局 B：50 个条带，物理顺序完全颠倒。
	specB := mkSpec()
	specB.rowsPerStrip = bigH / 50
	nStrips := 50
	specB.stripOrder = make([]int, nStrips)
	for i := range specB.stripOrder {
		specB.stripOrder[i] = nStrips - 1 - i
	}
	pathB := dir + "/shuffled_strips.tiff"
	writeTIFF(t, pathB, specB)

	// 布局 C：取向 6（交换宽高）+ 乱序条带，显示图像与 A/B 相同。
	specC := tiffSpec{
		width: bigH, height: bigW, spp: 3, orientation: 6,
		rowsPerStrip: bigW / 40,
		pixel: func(x, y int) (r, g, b, a uint8) {
			dx, dy := transform(6, bigH, bigW, x, y)
			return bigPixel(dx, dy)
		},
	}
	nStripsC := 40
	specC.stripOrder = make([]int, nStripsC)
	for i := range specC.stripOrder {
		specC.stripOrder[i] = (i*17 + 3) % nStripsC // 物理乱序
	}
	pathC := dir + "/orient6_shuffled.tiff"
	writeTIFF(t, pathC, specC)

	resA := runBigChild(t, pathA)
	resB := runBigChild(t, pathB)
	resC := runBigChild(t, pathC)
	t.Logf("single strip:     elapsed=%v peakRSS=%.1fMiB", resA.elapsed, float64(resA.maxRSS)/(1<<20))
	t.Logf("shuffled strips:  elapsed=%v peakRSS=%.1fMiB", resB.elapsed, float64(resB.maxRSS)/(1<<20))
	t.Logf("orient6 shuffled: elapsed=%v peakRSS=%.1fMiB", resC.elapsed, float64(resC.maxRSS)/(1<<20))

	for i, n := range Sizes {
		if !bytes.Equal(resA.pngs[i], resB.pngs[i]) {
			t.Errorf("size %d: single-strip and shuffled-strip outputs differ", n)
		}
		if !bytes.Equal(resA.pngs[i], resC.pngs[i]) {
			t.Errorf("size %d: single-strip and orient6-shuffled outputs differ", n)
		}
	}
	const limit = 30 * time.Second
	for name, res := range map[string]bigResult{"single": resA, "shuffled": resB, "orient6": resC} {
		if res.elapsed > limit {
			t.Errorf("%s: elapsed %v exceeds %v", name, res.elapsed, limit)
		}
		const maxMem = 96 << 20
		if res.maxRSS > maxMem {
			t.Errorf("%s: peak RSS %d bytes exceeds 96MiB", name, res.maxRSS)
		}
	}
}
