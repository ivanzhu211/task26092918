package tiffthumb

// Sizes 是三档缩略图的边长。
var Sizes = [3]int{150, 400, 800}

// cropWindow 描述显示坐标中的中心正方形裁剪窗口在某个目标尺寸下的参数。
// 所有边长均以 1/(2N) 像素为单位，使源像素边、裁剪边（可落在半像素处）
// 与输出像素边都是整数，从而权重为精确整数。
type cropWindow struct {
	n     int   // 目标边长 N
	cx2n  int64 // 裁剪左边界：N*(2*cx)
	cy2n  int64 // 裁剪上边界：N*(2*cy)
	twoL  int64 // 输出像素边长：2L（L 为裁剪边长，单位像素）
	total int64 // 每个输出像素的总权重：(2L)^2
}

// newCropWindow 构造裁剪窗口。cx2 = 2*cx、cy2 = 2*cy（整数，可为奇数），l 为裁剪边长。
func newCropWindow(n int, cx2, cy2, l int64) cropWindow {
	twoL := 2 * l
	return cropWindow{
		n:     n,
		cx2n:  int64(n) * cx2,
		cy2n:  int64(n) * cy2,
		twoL:  twoL,
		total: twoL * twoL,
	}
}

// accumulator 以精确整数权重累加一个目标尺寸的非预乘面积加权结果。
// 每个输出像素 4 个槽：sumWA、sumWAR、sumWAG、sumWAB，
// 其中 W 为面积权重，A 为原始 Alpha，R/G/B 为原始通道值。
type accumulator struct {
	win cropWindow
	buf []uint64
}

func newAccumulator(win cropWindow) *accumulator {
	return &accumulator{win: win, buf: make([]uint64, win.n*win.n*4)}
}

// addPixel 把显示坐标 (dx,dy) 处单位面积像素（RGBA 8 位）按覆盖面积权重累加。
func (a *accumulator) addPixel(dx, dy int, r, g, b, alpha uint8) {
	w := &a.win
	n := int64(w.n)
	// 源像素边（单位 1/(2N) 像素）。
	x0 := 2 * n * int64(dx)
	y0 := 2 * n * int64(dy)
	x1 := x0 + 2*n
	y1 := y0 + 2*n
	right := w.cx2n + n*w.twoL
	bottom := w.cy2n + n*w.twoL
	if x1 <= w.cx2n || x0 >= right || y1 <= w.cy2n || y0 >= bottom {
		return
	}
	// 覆盖的输出像素范围（闭区间）。
	oxMin := int64(0)
	if d := x0 - w.cx2n; d > 0 {
		oxMin = d / w.twoL
	}
	oxMax := n - 1
	if d := x1 - 1 - w.cx2n; d < oxMax*w.twoL {
		oxMax = d / w.twoL
	}
	oyMin := int64(0)
	if d := y0 - w.cy2n; d > 0 {
		oyMin = d / w.twoL
	}
	oyMax := n - 1
	if d := y1 - 1 - w.cy2n; d < oyMax*w.twoL {
		oyMax = d / w.twoL
	}
	wa := uint64(alpha)
	war := wa * uint64(r)
	wag := wa * uint64(g)
	wab := wa * uint64(b)
	for oy := oyMin; oy <= oyMax; oy++ {
		e0 := w.cy2n + oy*w.twoL
		yov := min(y1, e0+w.twoL) - max(y0, e0)
		row := oy * n
		for ox := oxMin; ox <= oxMax; ox++ {
			ex := w.cx2n + ox*w.twoL
			xov := min(x1, ex+w.twoL) - max(x0, ex)
			weight := uint64(xov * yov)
			idx := (row + ox) * 4
			a.buf[idx] += weight * wa
			a.buf[idx+1] += weight * war
			a.buf[idx+2] += weight * wag
			a.buf[idx+3] += weight * wab
		}
	}
}

// roundHalfUp 计算 round(num/den)，恰好一半时向上。den > 0。
func roundHalfUp(num, den uint64) uint64 {
	q, rem := num/den, num%den
	if 2*rem >= den {
		q++
	}
	return q
}

// finish 生成最终的非预乘 RGBA 像素（每像素 4 字节，共 n*n*4）。
func (a *accumulator) finish() []byte {
	w := &a.win
	den := uint64(w.total)
	out := make([]byte, w.n*w.n*4)
	for i := 0; i < w.n*w.n; i++ {
		sumWA := a.buf[i*4]
		if sumWA == 0 {
			continue // Alpha 为 0 时 RGB 必须为 0
		}
		out[i*4] = byte(roundHalfUp(a.buf[i*4+1], sumWA))
		out[i*4+1] = byte(roundHalfUp(a.buf[i*4+2], sumWA))
		out[i*4+2] = byte(roundHalfUp(a.buf[i*4+3], sumWA))
		out[i*4+3] = byte(roundHalfUp(sumWA, den))
	}
	return out
}
