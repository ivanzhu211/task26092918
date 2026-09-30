package tiffthumb

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
)

// ThumbSizes 为三档缩略图边长。
var ThumbSizes = [3]int{150, 400, 800}

// ReadInfo 读取 TIFF 的显示信息（还原取向后的宽高与分类）。
func ReadInfo(path string) (DisplayInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return DisplayInfo{}, err
	}
	defer f.Close()
	r, err := OpenTIFF(f)
	if err != nil {
		return DisplayInfo{}, err
	}
	w, h := displayDims(r.Width(), r.Height(), r.Orientation())
	return DisplayInfo{Width: w, Height: h, Category: categorize(w, h)}, nil
}

// GenerateThumbnails 对 tiffPath 的首个图像目录生成三档 PNG 缩略图，
// 依次写入 outPaths（长度须为 3，对应 150、400、800）。
// 不修改原图，除输出文件外不产生临时文件。
func GenerateThumbnails(tiffPath string, outPaths []string) error {
	if len(outPaths) != len(ThumbSizes) {
		return fmt.Errorf("tiffthumb: 需要 %d 个输出路径", len(ThumbSizes))
	}
	f, err := os.Open(tiffPath)
	if err != nil {
		return err
	}
	defer f.Close()
	r, err := OpenTIFF(f)
	if err != nil {
		return err
	}
	w, h, samples, orient := r.Width(), r.Height(), r.Samples(), r.Orientation()
	dispW, dispH := displayDims(w, h, orient)
	rs := newResampler(dispW, dispH, ThumbSizes)

	row := make([]byte, w*samples)
	for sy := 0; sy < h; sy++ {
		if err := r.ReadRow(sy, row); err != nil {
			return err
		}
		for sx := 0; sx < w; sx++ {
			p := row[sx*samples:]
			var alpha uint8 = 255
			if samples == 4 {
				alpha = p[3]
			}
			dx, dy := displayCoords(sx, sy, w, h, orient)
			rs.addPixel(dx, dy, p[0], p[1], p[2], alpha)
		}
	}

	for i, acc := range rs.accs {
		pix := acc.finish()
		img := &image.NRGBA{
			Pix:    pix,
			Stride: acc.size * 4,
			Rect:   image.Rect(0, 0, acc.size, acc.size),
		}
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		if err := enc.Encode(&buf, img); err != nil {
			return fmt.Errorf("tiffthumb: PNG 编码失败: %w", err)
		}
		if err := os.WriteFile(outPaths[i], buf.Bytes(), 0o644); err != nil {
			return fmt.Errorf("tiffthumb: 写入 %s 失败: %w", outPaths[i], err)
		}
	}
	return nil
}
