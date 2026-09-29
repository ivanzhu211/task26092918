package tiffthumb

import (
	"fmt"
	"image"
	"image/png"
	"os"
)

// ReadInfo 读取 TIFF 的显示信息（还原取向后的宽高与分类）。
func ReadInfo(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	h, err := readHeader(f)
	if err != nil {
		return Info{}, err
	}
	return h.info(), nil
}

// render 流式读取源图并生成三档非预乘 RGBA 位图（各 n*n*4 字节）。
func render(srcPath string) (Info, [3][]byte, error) {
	var out [3][]byte
	f, err := os.Open(srcPath)
	if err != nil {
		return Info{}, out, err
	}
	defer f.Close()
	h, err := readHeader(f)
	if err != nil {
		return Info{}, out, err
	}
	dw, dh := h.displaySize()
	l := int64(min(dw, dh))
	// 中心裁剪：边界可落在半像素处，用 2 倍坐标表示。
	cx2 := int64(dw) - l
	cy2 := int64(dh) - l
	accs := [3]*accumulator{}
	for i, n := range Sizes {
		accs[i] = newAccumulator(newCropWindow(n, cx2, cy2, l))
	}
	err = streamPixels(f, h, func(dx, dy int, r, g, b, a uint8) {
		for _, acc := range accs {
			acc.addPixel(dx, dy, r, g, b, a)
		}
	})
	if err != nil {
		return Info{}, out, err
	}
	for i, acc := range accs {
		out[i] = acc.finish()
	}
	return h.info(), out, nil
}

// encodePNG 把 n×n 非预乘 RGBA 像素编码为 PNG。
func encodePNG(pix []byte, n int) ([]byte, error) {
	img := &image.NRGBA{Pix: pix, Stride: n * 4, Rect: image.Rect(0, 0, n, n)}
	var buf pngBuffer
	enc := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.data, nil
}

// pngBuffer 是避免临时文件的简单字节缓冲。
type pngBuffer struct{ data []byte }

func (b *pngBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

// GenerateThumbnails 生成三档缩略图 PNG，写入调用方指定的三个路径
// （顺序对应 Sizes：150、400、800）。不修改原图，不产生其他临时文件。
func GenerateThumbnails(srcPath string, dstPaths [3]string) error {
	_, bitmaps, err := render(srcPath)
	if err != nil {
		return err
	}
	for i, pix := range bitmaps {
		data, err := encodePNG(pix, Sizes[i])
		if err != nil {
			return fmt.Errorf("encode %dx%d: %w", Sizes[i], Sizes[i], err)
		}
		if err := os.WriteFile(dstPaths[i], data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dstPaths[i], err)
		}
	}
	return nil
}
