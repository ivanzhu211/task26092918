package tiffthumb_test

import (
	"image"
	"image/png"
	"os"
)

// displayImage 返回还原取向后的显示像素，pix[y][x] = {r,g,b,a}。
func displayImage(spec tiffSpec) [][][4]uint8 {
	w, h, s := spec.width, spec.height, spec.samples
	dispW, dispH := w, h
	if spec.orient >= 5 && spec.orient <= 8 {
		dispW, dispH = h, w
	}
	out := make([][][4]uint8, dispH)
	for i := range out {
		out[i] = make([][4]uint8, dispW)
	}
	get := func(sx, sy int) [4]uint8 {
		p := spec.data[(sy*w+sx)*s:]
		a := uint8(255)
		if s == 4 {
			a = p[3]
		}
		return [4]uint8{p[0], p[1], p[2], a}
	}
	for sy := 0; sy < h; sy++ {
		for sx := 0; sx < w; sx++ {
			var dx, dy int
			switch spec.orient {
			case 2:
				dx, dy = w-1-sx, sy
			case 3:
				dx, dy = w-1-sx, h-1-sy
			case 4:
				dx, dy = sx, h-1-sy
			case 5:
				dx, dy = sy, sx
			case 6:
				dx, dy = h-1-sy, sx
			case 7:
				dx, dy = h-1-sy, w-1-sx
			case 8:
				dx, dy = sy, w-1-sx
			default:
				dx, dy = sx, sy
			}
			out[dy][dx] = get(sx, sy)
		}
	}
	return out
}

// refThumbnail 是独立的精确面积加权参考实现。
// 与库实现（按源像素散射）不同，这里按输出像素聚集：
// 每个输出像素直接枚举其矩形覆盖的源像素，全部用整数运算，
// 坐标以 1/(2*size) 源像素为单位，四舍五入、半值向上。
func refThumbnail(disp [][][4]uint8, size int) *image.NRGBA {
	dispH := len(disp)
	dispW := len(disp[0])
	side := dispW
	if dispH < side {
		side = dispH
	}
	s2 := 2 * size
	cx0 := (dispW - side) * size // 1/(2S) 单位
	cy0 := (dispH - side) * size
	cell := 2 * side

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for oy := 0; oy < size; oy++ {
		ry0 := cy0 + oy*cell
		ry1 := ry0 + cell
		for ox := 0; ox < size; ox++ {
			rx0 := cx0 + ox*cell
			rx1 := rx0 + cell
			var sumA, sumR, sumG, sumB int64
			// 覆盖的源像素范围（源像素为单位正方形）。
			y0 := ry0 / s2
			y1 := (ry1 - 1) / s2
			x0 := rx0 / s2
			x1 := (rx1 - 1) / s2
			for sy := y0; sy <= y1; sy++ {
				wy := minI(ry1, (sy+1)*s2) - maxI(ry0, sy*s2)
				if wy <= 0 {
					continue
				}
				for sx := x0; sx <= x1; sx++ {
					wx := minI(rx1, (sx+1)*s2) - maxI(rx0, sx*s2)
					if wx <= 0 {
						continue
					}
					w := int64(wx) * int64(wy)
					px := disp[sy][sx]
					a := int64(px[3])
					sumA += w * a
					sumR += w * a * int64(px[0])
					sumG += w * a * int64(px[1])
					sumB += w * a * int64(px[2])
				}
			}
			i := img.PixOffset(ox, oy)
			den := int64(s2) * int64(s2)
			av := roundHalfUpInt(sumA, den)
			if av == 0 {
				continue
			}
			img.Pix[i+0] = uint8(roundHalfUpInt(sumR, sumA))
			img.Pix[i+1] = uint8(roundHalfUpInt(sumG, sumA))
			img.Pix[i+2] = uint8(roundHalfUpInt(sumB, sumA))
			img.Pix[i+3] = uint8(av)
		}
	}
	return img
}

// roundHalfUpInt 计算 round(num/den)，恰好一半时向上。num,den >= 0，den > 0。
func roundHalfUpInt(num, den int64) int64 {
	return (2*num + den) / (2 * den)
}

func minI(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// decodePNGs 解码三档 PNG 输出。
func decodePNGs(t interface{ Fatalf(string, ...any) }, paths []string) []*image.NRGBA {
	imgs := make([]*image.NRGBA, len(paths))
	for i, p := range paths {
		img, err := readPNG(p)
		if err != nil {
			t.Fatalf("解码 %s 失败: %v", p, err)
		}
		imgs[i] = img
	}
	return imgs
}

func readPNG(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	if nrgba, ok := img.(*image.NRGBA); ok {
		return nrgba, nil
	}
	b := img.Bounds()
	n := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			n.Set(x, y, img.At(x, y))
		}
	}
	return n, nil
}

func samePixels(a, b *image.NRGBA) (diff int, first string) {
	if a.Bounds() != b.Bounds() {
		return -1, "尺寸不同"
	}
	n := 0
	firstMsg := ""
	w := a.Bounds().Dx()
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			n++
			if firstMsg == "" {
				p := i / 4
				firstMsg = "像素(" + itoa(p%w) + "," + itoa(p/w) + ")"
			}
		}
	}
	return n, firstMsg
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
