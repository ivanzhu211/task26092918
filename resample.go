package tiffthumb

// accumulator 保存某档缩略图的精确整数累加器。
//
// 所有坐标以 1/(2*size) 源像素为单位，因此裁剪起点（可能落在半像素处）
// 与输出像素边界均为整数。源像素为单位正方形，输出像素边长为 2*side 个单位。
// 每个输出像素记录 4 个整数和：
//
//	A  = Σ w*a
//	R  = Σ w*a*r, G、B 同理
//
// 其中 w = 覆盖面积（单位平方），Σw 恰为 (2*size)^2。
type accumulator struct {
	size  int   // 输出边长 S
	side  int   // 裁剪正方形边长（源像素单位）
	cx0   int   // 裁剪区域左边界，1/(2S) 单位
	cy0   int   // 裁剪区域上边界，1/(2S) 单位
	data  []int64
}

func newAccumulator(size, side, cx0, cy0 int) *accumulator {
	return &accumulator{
		size: size,
		side: side,
		cx0:  cx0,
		cy0:  cy0,
		data: make([]int64, size*size*4),
	}
}

// addPixel 将显示坐标 (dx,dy) 处的单位源像素散射到重叠的输出像素。
func (a *accumulator) addPixel(dx, dy int, r, g, b, alpha uint8) {
	s2 := 2 * a.size
	x0 := dx * s2
	x1 := x0 + s2
	y0 := dy * s2
	y1 := y0 + s2
	cell := 2 * a.side
	// 输出像素 j 的横向跨度: [cx0 + j*cell, cx0 + (j+1)*cell)
	jlo := (x0 - a.cx0) / cell
	if jlo < 0 {
		jlo = 0
	}
	jhi := (x1 - 1 - a.cx0) / cell
	if jhi >= a.size {
		jhi = a.size - 1
	}
	if jhi < jlo {
		return
	}
	klo := (y0 - a.cy0) / cell
	if klo < 0 {
		klo = 0
	}
	khi := (y1 - 1 - a.cy0) / cell
	if khi >= a.size {
		khi = a.size - 1
	}
	if khi < klo {
		return
	}
	av := int64(alpha)
	rv := av * int64(r)
	gv := av * int64(g)
	bv := av * int64(b)
	for k := klo; k <= khi; k++ {
		oy := minInt(y1, a.cy0+(k+1)*cell) - maxInt(y0, a.cy0+k*cell)
		if oy <= 0 {
			continue
		}
		row := a.data[k*a.size*4:]
		for j := jlo; j <= jhi; j++ {
			ox := minInt(x1, a.cx0+(j+1)*cell) - maxInt(x0, a.cx0+j*cell)
			if ox <= 0 {
				continue
			}
			w := int64(ox) * int64(oy)
			p := row[j*4:]
			p[0] += w * av
			p[1] += w * rv
			p[2] += w * gv
			p[3] += w * bv
		}
	}
}

// roundHalfUp 计算 round(num/den)，恰好一半时向上。num,den >= 0，den > 0。
func roundHalfUp(num, den int64) int64 {
	return (2*num + den) / (2 * den)
}

// finish 计算最终非预乘 RGBA 像素，每像素 4 字节。
// Alpha 为 0 的像素 RGB 全为 0。
func (a *accumulator) finish() []byte {
	out := make([]byte, a.size*a.size*4)
	den := int64(2 * a.size)
	den *= den
	for i := 0; i < a.size*a.size; i++ {
		s := a.data[i*4:]
		d := out[i*4:]
		alpha := roundHalfUp(s[0], den)
		if alpha <= 0 {
			continue
		}
		d[0] = byte(roundHalfUp(s[1], s[0]))
		d[1] = byte(roundHalfUp(s[2], s[0]))
		d[2] = byte(roundHalfUp(s[3], s[0]))
		d[3] = byte(alpha)
	}
	return out
}

// resampler 将显示坐标下的源像素散射到三档累加器。
type resampler struct {
	accs [3]*accumulator
}

func newResampler(dispW, dispH int, sizes [3]int) *resampler {
	side := minInt(dispW, dispH)
	rs := &resampler{}
	for i, s := range sizes {
		// 裁剪起点（可能为半像素）换算为 1/(2S) 单位，结果为整数。
		cx0 := (dispW - side) * s
		cy0 := (dispH - side) * s
		rs.accs[i] = newAccumulator(s, side, cx0, cy0)
	}
	return rs
}

// addPixel 加入显示坐标 (dx,dy) 处单位面积的源像素。
func (rs *resampler) addPixel(dx, dy int, r, g, b, a uint8) {
	for _, acc := range rs.accs {
		acc.addPixel(dx, dy, r, g, b, a)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
