package tiffthumb

// 本文件是验收用的独立参考实现：自带 TIFF 解码（整图读入内存），
// 重采样使用 big.Rat 精确有理数做面积加权，与库实现完全独立。

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"math/big"
	"os"
	"testing"
)

func decodePNGFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	pix := make([]byte, w*h*4)
	switch im := img.(type) {
	case *image.NRGBA: // 带 alpha：编码器保留原始非预乘值
		for y := 0; y < h; y++ {
			copy(pix[y*w*4:], im.Pix[y*im.Stride:y*im.Stride+w*4])
		}
	case *image.RGBA: // 全不透明：编码器写为 RGB，alpha 视为 255
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				s := im.Pix[y*im.Stride+x*4:]
				d := pix[(y*w+x)*4:]
				d[0], d[1], d[2], d[3] = s[0], s[1], s[2], 255
			}
		}
	default:
		t.Fatalf("%s: unexpected decoded PNG type %T", path, img)
	}
	return pix
}

// refTIFF 是参考解码器读出的整图（显示坐标，非预乘 RGBA）。
type refTIFF struct {
	dw, dh int
	orient int
	pix    []byte // 显示坐标 NRGBA
}

// refLoadTIFF 独立解析 TIFF 并还原取向（整图驻留内存，仅用于小图测试）。
func refLoadTIFF(t *testing.T, path string) refTIFF {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var bo binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		t.Fatal("bad byte order")
	}
	if bo.Uint16(data[2:4]) != 42 {
		t.Fatal("bad magic")
	}
	ifd := int(bo.Uint32(data[4:8]))
	n := int(bo.Uint16(data[ifd:]))
	tags := map[uint16][]uint32{}
	for i := 0; i < n; i++ {
		e := data[ifd+2+i*12:]
		tag := bo.Uint16(e[0:2])
		typ := bo.Uint16(e[2:4])
		cnt := int(bo.Uint32(e[4:8]))
		sz := 4
		if typ == 3 {
			sz = 2
		}
		var raw []byte
		if cnt*sz <= 4 {
			raw = e[8 : 8+cnt*sz]
		} else {
			off := int(bo.Uint32(e[8:12]))
			raw = data[off : off+cnt*sz]
		}
		vals := make([]uint32, cnt)
		for j := range vals {
			if typ == 3 {
				vals[j] = uint32(bo.Uint16(raw[j*2:]))
			} else {
				vals[j] = bo.Uint32(raw[j*4:])
			}
		}
		tags[tag] = vals
	}
	get := func(tag uint16, def uint32) uint32 {
		if v, ok := tags[tag]; ok && len(v) > 0 {
			return v[0]
		}
		return def
	}
	w := int(get(tagImageWidth, 0))
	h := int(get(tagImageLength, 0))
	orient := int(get(tagOrientation, 1))
	spp := int(get(tagSamplesPerPixel, 3))
	rps := int(get(tagRowsPerStrip, uint32(h)))
	offs := tags[tagStripOffsets]
	cnts := tags[tagStripByteCounts]

	src := make([]byte, w*h*spp)
	for si, so := range offs {
		rows := min(rps, h-si*rps)
		copy(src[si*rps*w*spp:], data[so:so+uint32(rows*w*spp)])
		_ = cnts
	}
	dw, dh := w, h
	if orient >= 5 {
		dw, dh = h, w
	}
	out := refTIFF{dw: dw, dh: dh, orient: orient, pix: make([]byte, dw*dh*4)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := src[(y*w+x)*spp:]
			var a uint8 = 255
			if spp == 4 {
				a = p[3]
			}
			dx, dy := refTransform(orient, w, h, x, y)
			q := out.pix[(dy*dw+dx)*4:]
			q[0], q[1], q[2], q[3] = p[0], p[1], p[2], a
		}
	}
	return out
}

// refTransform 独立的取向映射（与库实现分开书写）。
func refTransform(orient, w, h, x, y int) (int, int) {
	switch orient {
	case 1:
		return x, y
	case 2:
		return w - 1 - x, y
	case 3:
		return w - 1 - x, h - 1 - y
	case 4:
		return x, h - 1 - y
	case 5:
		return y, x
	case 6:
		return h - 1 - y, x
	case 7:
		return h - 1 - y, w - 1 - x
	case 8:
		return y, w - 1 - x
	}
	panic("bad orientation")
}

var (
	ratZero = big.NewRat(0, 1)
	ratHalf = big.NewRat(1, 2)
)

// roundHalfUpRat 对非负有理数四舍五入，半值向上。
func roundHalfUpRat(x *big.Rat) uint64 {
	y := new(big.Rat).Add(x, ratHalf)
	q := new(big.Int).Quo(y.Num(), y.Denom()) // 非负，Quo 即向下取整
	return q.Uint64()
}

// refThumb 对显示图像计算 n×n 精确面积加权缩略图（非预乘 RGBA）。
func refThumb(img refTIFF, n int) []byte {
	l := min(img.dw, img.dh)
	// 裁剪起点（可为半像素），用有理数表示。
	cx := big.NewRat(int64(img.dw-l), 2)
	cy := big.NewRat(int64(img.dh-l), 2)
	scale := big.NewRat(int64(l), int64(n)) // 每个输出像素的边长
	total := new(big.Rat).Mul(scale, scale)

	out := make([]byte, n*n*4)
	for oy := 0; oy < n; oy++ {
		for ox := 0; ox < n; ox++ {
			x0 := new(big.Rat).Add(cx, new(big.Rat).Mul(big.NewRat(int64(ox), 1), scale))
			y0 := new(big.Rat).Add(cy, new(big.Rat).Mul(big.NewRat(int64(oy), 1), scale))
			x1 := new(big.Rat).Add(x0, scale)
			y1 := new(big.Rat).Add(y0, scale)
			sumWA := new(big.Rat)
			sumWR := new(big.Rat)
			sumWG := new(big.Rat)
			sumWB := new(big.Rat)
			// 覆盖的源像素范围。
			sx0 := intFloorRat(x0)
			sx1 := intCeilRat(x1)
			sy0 := intFloorRat(y0)
			sy1 := intCeilRat(y1)
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					lo := maxRat(x0, big.NewRat(int64(sx), 1))
					hi := minRat(x1, big.NewRat(int64(sx+1), 1))
					wox := new(big.Rat).Sub(hi, lo)
					if wox.Sign() <= 0 {
						continue
					}
					loy := maxRat(y0, big.NewRat(int64(sy), 1))
					hiy := minRat(y1, big.NewRat(int64(sy+1), 1))
					woy := new(big.Rat).Sub(hiy, loy)
					if woy.Sign() <= 0 {
						continue
					}
					wgt := new(big.Rat).Mul(wox, woy)
					p := img.pix[(sy*img.dw+sx)*4:]
					wa := new(big.Rat).Mul(wgt, big.NewRat(int64(p[3]), 1))
					sumWA.Add(sumWA, wa)
					sumWR.Add(sumWR, new(big.Rat).Mul(wa, big.NewRat(int64(p[0]), 1)))
					sumWG.Add(sumWG, new(big.Rat).Mul(wa, big.NewRat(int64(p[1]), 1)))
					sumWB.Add(sumWB, new(big.Rat).Mul(wa, big.NewRat(int64(p[2]), 1)))
				}
			}
			q := out[(oy*n+ox)*4:]
			if sumWA.Cmp(ratZero) == 0 {
				continue // Alpha 0 → RGB 全 0
			}
			q[0] = byte(roundHalfUpRat(new(big.Rat).Quo(sumWR, sumWA)))
			q[1] = byte(roundHalfUpRat(new(big.Rat).Quo(sumWG, sumWA)))
			q[2] = byte(roundHalfUpRat(new(big.Rat).Quo(sumWB, sumWA)))
			q[3] = byte(roundHalfUpRat(new(big.Rat).Quo(sumWA, total)))
		}
	}
	return out
}

func intFloorRat(x *big.Rat) int {
	q := new(big.Int).Quo(x.Num(), x.Denom()) // 截断
	if x.Sign() < 0 && new(big.Int).Rem(x.Num(), x.Denom()).Sign() != 0 {
		q.Sub(q, big.NewInt(1))
	}
	return int(q.Int64())
}

func intCeilRat(x *big.Rat) int {
	f := intFloorRat(x)
	if big.NewRat(int64(f), 1).Cmp(x) == 0 {
		return f
	}
	return f + 1
}

func minRat(a, b *big.Rat) *big.Rat {
	if a.Cmp(b) <= 0 {
		return a
	}
	return b
}

func maxRat(a, b *big.Rat) *big.Rat {
	if a.Cmp(b) >= 0 {
		return a
	}
	return b
}
