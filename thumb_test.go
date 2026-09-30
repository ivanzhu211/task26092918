package tiffthumb_test

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"

	"tiffthumb"
)

// runCase 对单个 TIFF 规格执行：读信息 + 生成三档缩略图 + 与参考逐像素比较。
func runCase(t *testing.T, spec tiffSpec) {
	t.Helper()
	path := writeTIFF(t, spec)

	info, err := tiffthumb.ReadInfo(path)
	if err != nil {
		t.Fatalf("ReadInfo 失败: %v", err)
	}
	disp := displayImage(spec)
	wantW, wantH := len(disp[0]), len(disp)
	if info.Width != wantW || info.Height != wantH {
		t.Errorf("显示尺寸 = %dx%d, 期望 %dx%d", info.Width, info.Height, wantW, wantH)
	}
	wantCat := tiffthumb.Square
	if wantW > wantH {
		wantCat = tiffthumb.Landscape
	} else if wantW < wantH {
		wantCat = tiffthumb.Portrait
	}
	if info.Category != wantCat {
		t.Errorf("分类 = %v, 期望 %v", info.Category, wantCat)
	}

	dir := t.TempDir()
	outs := make([]string, 3)
	for i, s := range tiffthumb.ThumbSizes {
		outs[i] = filepath.Join(dir, fmt.Sprintf("t%d.png", s))
	}
	if err := tiffthumb.GenerateThumbnails(path, outs); err != nil {
		t.Fatalf("GenerateThumbnails 失败: %v", err)
	}
	got := decodePNGs(t, outs)
	for i, s := range tiffthumb.ThumbSizes {
		ref := refThumbnail(disp, s)
		if n, first := samePixels(got[i], ref); n != 0 {
			t.Errorf("尺寸 %d: %d 个像素不一致，首个在 %s", s, n, first)
		}
	}
}

// randPixels 生成确定性伪随机像素。
func randPixels(seed int64, w, h, samples int) []byte {
	rng := rand.New(rand.NewSource(seed))
	data := make([]byte, w*h*samples)
	for i := range data {
		data[i] = byte(rng.Intn(256))
	}
	return data
}

// TestOrientations 覆盖全部 8 种取向 × 两种字节序，RGB 与 RGBA。
func TestOrientations(t *testing.T) {
	for orient := 1; orient <= 8; orient++ {
		for _, bigEndian := range []bool{false, true} {
			for _, samples := range []int{3, 4} {
				name := fmt.Sprintf("orient%d/be=%v/s%d", orient, bigEndian, samples)
				t.Run(name, func(t *testing.T) {
					w, h := 7, 5
					runCase(t, tiffSpec{
						bigEndian:    bigEndian,
						width:        w,
						height:       h,
						samples:      samples,
						orient:       orient,
						rowsPerStrip: 2,
						data:         randPixels(int64(orient*100+samples), w, h, samples),
					})
				})
			}
		}
	}
}

// TestOddSizes 奇数边长导致裁剪边界落在半像素处。
func TestOddSizes(t *testing.T) {
	for _, wh := range [][2]int{{5, 3}, {3, 5}, {9, 7}, {1, 1}, {1, 4}, {4, 1}} {
		w, h := wh[0], wh[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			runCase(t, tiffSpec{
				width:        w,
				height:       h,
				samples:      4,
				orient:       1,
				rowsPerStrip: 1,
				data:         randPixels(int64(w*31+h), w, h, 4),
			})
		})
	}
}

// TestAlphaEdges 全透明强色像素与半透明像素相邻：
// 透明像素的颜色不得泄漏，Alpha=0 的输出像素 RGB 必须为 0。
func TestAlphaEdges(t *testing.T) {
	// 4x4：左半全透明纯红，右半半透明绿色。
	w, h := 4, 4
	data := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := data[(y*w+x)*4:]
			if x < 2 {
				p[0], p[1], p[2], p[3] = 255, 0, 0, 0
			} else {
				p[0], p[1], p[2], p[3] = 0, 200, 0, 128
			}
		}
	}
	runCase(t, tiffSpec{
		width: w, height: h, samples: 4, orient: 1,
		rowsPerStrip: 3, data: data,
	})

	// 全部像素完全透明：所有输出像素 RGBA 必须全 0。
	data2 := make([]byte, w*h*4)
	for i := 0; i < w*h; i++ {
		data2[i*4+0] = 255
		data2[i*4+1] = 30
	}
	runCase(t, tiffSpec{
		width: w, height: h, samples: 4, orient: 3,
		rowsPerStrip: 2, data: data2,
	})
}

// TestStrips 条带物理乱序与不足标准行数的末条带。
func TestStrips(t *testing.T) {
	w, h := 6, 7
	data := randPixels(42, w, h, 4)
	base := tiffSpec{
		width: w, height: h, samples: 4, orient: 6,
		rowsPerStrip: 3, data: data,
	}
	t.Run("顺序", func(t *testing.T) { runCase(t, base) })
	shuffled := base
	shuffled.stripOrder = []int{2, 0, 1}
	t.Run("乱序", func(t *testing.T) { runCase(t, shuffled) })
}
