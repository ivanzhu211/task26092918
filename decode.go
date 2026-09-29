package tiffthumb

import (
	"fmt"
	"io"
)

// pixelFunc 接收显示坐标 (dx,dy) 处的非预乘 RGBA 像素。
type pixelFunc func(dx, dy int, r, g, b, a uint8)

// transform 把源坐标 (x,y) 按取向映射为显示坐标。
func transform(orient, w, h, x, y int) (int, int) {
	switch orient {
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
	default: // 1
		return x, y
	}
}

// streamPixels 按条带流式读取图像，以显示坐标顺序无关地回调每个像素。
// 内存占用与图像尺寸无关（仅一个条带分块缓冲）。
func streamPixels(r io.ReaderAt, h *tiffHeader, fn pixelFunc) error {
	spp := h.samplesPerPx
	rowBytes := int64(h.width * spp)
	const chunkSize = int64(4 << 20)
	bufRows := int(chunkSize / rowBytes)
	if bufRows < 1 {
		bufRows = 1
	}
	buf := make([]byte, int(rowBytes)*bufRows)

	emit := func(x, y int, p []byte) {
		dx, dy := transform(h.orientation, h.width, h.height, x, y)
		if spp == 4 {
			fn(dx, dy, p[0], p[1], p[2], p[3])
		} else {
			fn(dx, dy, p[0], p[1], p[2], 255)
		}
	}

	for si := range h.stripOffsets {
		rowStart := si * h.rowsPerStrip
		rowEnd := min(rowStart+h.rowsPerStrip, h.height)
		off := int64(h.stripOffsets[si])
		remaining := int64(h.stripByteCnts[si])
		for y := rowStart; y < rowEnd; {
			nRows := min(bufRows, rowEnd-y)
			want := int64(nRows) * rowBytes
			if want > remaining {
				return fmt.Errorf("strip %d: short data (need %d more bytes, have %d)", si, want, remaining)
			}
			if _, err := r.ReadAt(buf[:want], off); err != nil {
				return fmt.Errorf("strip %d: read rows: %w", si, err)
			}
			for j := 0; j < nRows; j++ {
				row := buf[int64(j)*rowBytes : int64(j+1)*rowBytes]
				yy := y + j
				for x := 0; x < h.width; x++ {
					emit(x, yy, row[x*spp:])
				}
			}
			y += nRows
			off += want
			remaining -= want
		}
	}
	return nil
}
