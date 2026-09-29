package tiffthumb

import (
	"bytes"
	"fmt"
	"testing"
)

// patternPixel 生成确定性的测试图案（显示坐标），Alpha 覆盖 0、半透明与不透明。
func patternPixel(x, y int) (r, g, b, a uint8) {
	r = uint8((x*37 + y*17 + 5) % 256)
	g = uint8((x*11 + y*53 + 7) % 256)
	b = uint8((x*x + y*29 + 3) % 256)
	a = uint8((x*13 + y*41 + 200) % 256)
	return
}

// displayToSourceSpec 返回一个 tiffSpec，其还原取向后的显示图像等于 pattern。
func displayToSourceSpec(w, h, orient, spp int, pattern func(x, y int) (r, g, b, a uint8)) tiffSpec {
	// 源坐标下的存储尺寸。
	sw, sh := w, h
	if orient >= 5 {
		sw, sh = h, w
	}
	return tiffSpec{
		width:  sw,
		height: sh,
		spp:    spp,
		// orientation 由调用方设置
		pixel: func(x, y int) (r, g, b, a uint8) {
			dx, dy := transform(orient, sw, sh, x, y)
			return pattern(dx, dy)
		},
	}
}

func checkAgainstReference(t *testing.T, path string, wantInfo Info) {
	t.Helper()
	info, pngs := generatePNGs(t, path)
	if info != wantInfo {
		t.Errorf("info = %+v, want %+v", info, wantInfo)
	}
	ref := refLoadTIFF(t, path)
	for i, n := range Sizes {
		want := refThumb(ref, n)
		if !bytes.Equal(pngs[i], want) {
			nBad := 0
			for p := 0; p < len(want); p += 4 {
				if !bytes.Equal(pngs[i][p:p+4], want[p:p+4]) {
					if nBad < 3 {
						t.Errorf("size %d pixel (%d,%d) = %v, want %v",
							n, (p/4)%n, (p/4)/n, pngs[i][p:p+4], want[p:p+4])
					}
					nBad++
				}
			}
			t.Errorf("size %d: %d pixels differ from reference", n, nBad)
		}
	}
}

func expectedInfo(w, h, orient int) Info {
	dw, dh := w, h
	if orient >= 5 {
		dw, dh = h, w
	}
	return expectedInfoDisplay(dw, dh, orient)
}

// expectedInfoDisplay 由显示尺寸构造期望的 Info。
func expectedInfoDisplay(dw, dh, orient int) Info {
	shape := ShapeSquare
	if dw > dh {
		shape = ShapeLandscape
	} else if dw < dh {
		shape = ShapePortrait
	}
	return Info{Width: dw, Height: dh, Shape: shape, Orientation: orient}
}

// TestReferenceExact 小图与独立 big.Rat 参考逐像素一致。
func TestReferenceExact(t *testing.T) {
	cases := []struct {
		name   string
		w, h   int
		spp    int
		orient int
		be     bool
		rps    int
	}{
		{"rgba_9x6_odd_crop_le", 9, 6, 4, 1, false, 4}, // 半像素裁剪 cx=1.5，末条带不足
		{"rgb_8x8_be", 8, 8, 3, 1, true, 0},
		{"rgba_6x9_portrait_odd_crop", 6, 9, 4, 1, false, 0}, // 半像素裁剪 cy=1.5
		{"rgba_9x6_orient6", 9, 6, 4, 6, false, 0},           // 交换宽高的取向
		{"rgba_3x2_tiny_upscale", 3, 2, 4, 1, false, 0},
		{"rgb_7x5_odd_dims", 7, 5, 3, 1, false, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := displayToSourceSpec(tc.w, tc.h, tc.orient, tc.spp, patternPixel)
			spec.bigEndian = tc.be
			spec.orientation = tc.orient
			spec.rowsPerStrip = tc.rps
			path := fmt.Sprintf("%s/%s.tiff", t.TempDir(), tc.name)
			writeTIFF(t, path, spec)
			checkAgainstReference(t, path, expectedInfoDisplay(tc.w, tc.h, tc.orient))
		})
	}
}

// TestOrientations 全部 8 种取向还原同一显示图像，结果必须与取向 1 完全一致。
func TestOrientations(t *testing.T) {
	w, h := 9, 6
	var base [3][]byte
	for orient := 1; orient <= 8; orient++ {
		t.Run(fmt.Sprintf("orient_%d", orient), func(t *testing.T) {
			spec := displayToSourceSpec(w, h, orient, 4, patternPixel)
			spec.orientation = orient
			path := fmt.Sprintf("%s/o%d.tiff", t.TempDir(), orient)
			writeTIFF(t, path, spec)
			info, pngs := generatePNGs(t, path)
			if want := expectedInfoDisplay(w, h, orient); info != want {
				t.Fatalf("info = %+v, want %+v", info, want)
			}
			if orient == 1 {
				base = pngs
				return
			}
			for i, n := range Sizes {
				if !bytes.Equal(pngs[i], base[i]) {
					t.Errorf("size %d: orientation %d output differs from orientation 1", n, orient)
				}
			}
		})
	}
}

// TestDefaultOrientation 缺省 Orientation 标签视为 1。
func TestDefaultOrientation(t *testing.T) {
	spec := displayToSourceSpec(8, 6, 1, 3, patternPixel)
	spec.orientation = 0 // 不写标签
	path := fmt.Sprintf("%s/noorient.tiff", t.TempDir())
	writeTIFF(t, path, spec)
	info := mustInfo(t, path)
	if info.Orientation != 1 {
		t.Fatalf("default orientation = %d, want 1", info.Orientation)
	}
	checkAgainstReference(t, path, expectedInfo(8, 6, 1))
}

// TestByteOrdersAndStripLayouts 同一显示图像在不同字节序与条带布局下结果一致。
func TestByteOrdersAndStripLayouts(t *testing.T) {
	w, h := 9, 6
	mk := func(name string, mutate func(*tiffSpec)) (Info, [3][]byte) {
		spec := displayToSourceSpec(w, h, 1, 4, patternPixel)
		spec.orientation = 1
		mutate(&spec)
		path := fmt.Sprintf("%s/%s.tiff", t.TempDir(), name)
		writeTIFF(t, path, spec)
		return generatePNGs(t, path)
	}
	_, base := mk("base_le_single_strip", func(s *tiffSpec) {})
	variants := []struct {
		name   string
		mutate func(*tiffSpec)
	}{
		{"big_endian", func(s *tiffSpec) { s.bigEndian = true }},
		{"multi_strip", func(s *tiffSpec) { s.rowsPerStrip = 2 }},
		{"short_last_strip", func(s *tiffSpec) { s.rowsPerStrip = 5 }},
		{"shuffled_strips", func(s *tiffSpec) {
			s.rowsPerStrip = 2
			s.stripOrder = []int{2, 0, 1} // 物理乱序
		}},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			_, pngs := mk(v.name, v.mutate)
			for i, n := range Sizes {
				if !bytes.Equal(pngs[i], base[i]) {
					t.Errorf("size %d: %s output differs from baseline", n, v.name)
				}
			}
		})
	}
}

// TestTransparentStrongColor 全透明强色像素不得污染相邻半透明像素；Alpha 0 输出 RGB 必须为 0。
func TestTransparentStrongColor(t *testing.T) {
	// 2×1：左=全透明纯红，右=半透明蓝。
	spec := tiffSpec{
		width: 2, height: 1, spp: 4, orientation: 1,
		pixel: func(x, y int) (r, g, b, a uint8) {
			if x == 0 {
				return 255, 0, 0, 0
			}
			return 0, 0, 255, 128
		},
	}
	path := fmt.Sprintf("%s/alpha.tiff", t.TempDir())
	writeTIFF(t, path, spec)
	checkAgainstReference(t, path, expectedInfo(2, 1, 1))

	_, pngs := generatePNGs(t, path)
	n := Sizes[0]
	px := func(x, y int) []byte { return pngs[0][(y*n+x)*4 : (y*n+x)*4+4] }
	if got := px(0, 0); !bytes.Equal(got, []byte{0, 0, 0, 0}) {
		t.Errorf("transparent region = %v, want [0 0 0 0] (no red bleed)", got)
	}
	if got := px(n-1, n-1); !bytes.Equal(got, []byte{0, 0, 255, 128}) {
		t.Errorf("semi-transparent region = %v, want [0 0 255 128]", got)
	}
}

// TestHalfPixelCrop 奇数边长差使裁剪边界落在半像素处（由参考实现锚定）。
func TestHalfPixelCrop(t *testing.T) {
	// 5×8：L=5，cx=1.5；8×5：L=5，cy=1.5。
	for _, wh := range [][2]int{{5, 8}, {8, 5}} {
		w, h := wh[0], wh[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			spec := displayToSourceSpec(w, h, 1, 4, patternPixel)
			spec.orientation = 1
			path := fmt.Sprintf("%s/half.tiff", t.TempDir())
			writeTIFF(t, path, spec)
			checkAgainstReference(t, path, expectedInfo(w, h, 1))
		})
	}
}
