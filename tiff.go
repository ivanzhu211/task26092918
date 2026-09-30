package tiffthumb

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Reader 提供对经典 TIFF 首个图像目录的随机行访问。
// 仅支持无压缩、分条带、8 位交错 RGB/RGBA。
type Reader struct {
	ra       io.ReaderAt
	order    binary.ByteOrder
	width    int
	height   int
	samples  int // 3=RGB, 4=RGBA
	orient   int
	rowsPer  int
	offsets  []uint64
	counts   []uint64
	rowBytes uint64
}

// Width/Height 为文件中存储的源图尺寸（未还原取向）。
func (r *Reader) Width() int  { return r.width }
func (r *Reader) Height() int { return r.height }

// Samples 返回每像素采样数：3 为 RGB，4 为 RGBA（非预乘）。
func (r *Reader) Samples() int { return r.samples }

// Orientation 返回 Orientation 标记，缺省为 1。
func (r *Reader) Orientation() int { return r.orient }

const (
	tagWidth          = 256
	tagHeight         = 257
	tagBitsPerSample  = 258
	tagCompression    = 259
	tagPhotometric    = 262
	tagOrientation    = 274
	tagStripOffsets   = 273
	tagSamplesPerPix  = 277
	tagRowsPerStrip   = 278
	tagStripByteCnts  = 279
	tagPlanarConfig   = 284
	tagExtraSamples   = 338
)

// OpenTIFF 解析 TIFF 头与首个 IFD，返回可随机读行的 Reader。
func OpenTIFF(ra io.ReaderAt) (*Reader, error) {
	var hdr [8]byte
	if _, err := ra.ReadAt(hdr[:], 0); err != nil {
		return nil, fmt.Errorf("tiffthumb: 读取文件头失败: %w", err)
	}
	r := &Reader{ra: ra, orient: 1, samples: 3}
	switch {
	case hdr[0] == 'I' && hdr[1] == 'I':
		r.order = binary.LittleEndian
	case hdr[0] == 'M' && hdr[1] == 'M':
		r.order = binary.BigEndian
	default:
		return nil, fmt.Errorf("tiffthumb: 非法字节序标记")
	}
	if magic := r.order.Uint16(hdr[2:4]); magic != 42 {
		return nil, fmt.Errorf("tiffthumb: 非法 TIFF 魔数 %d", magic)
	}
	ifdOff := int64(r.order.Uint32(hdr[4:8]))
	var cntBuf [2]byte
	if _, err := ra.ReadAt(cntBuf[:], ifdOff); err != nil {
		return nil, fmt.Errorf("tiffthumb: 读取 IFD 失败: %w", err)
	}
	n := int(r.order.Uint16(cntBuf[:]))
	entries := make([]byte, 12*n)
	if _, err := ra.ReadAt(entries, ifdOff+2); err != nil {
		return nil, fmt.Errorf("tiffthumb: 读取 IFD 条目失败: %w", err)
	}
	var (
		compression, photometric, planar uint16 = 1, 2, 1
		bps                              []uint16
		extra                            []uint16
		hasExtra                         bool
		rowsPer                          uint32
	)
	for i := 0; i < n; i++ {
		e := entries[i*12:]
		tag := r.order.Uint16(e[0:2])
		typ := r.order.Uint16(e[2:4])
		cnt := r.order.Uint32(e[4:8])
		val := e[8:12]
		u16s, err := r.readU16s(typ, cnt, val)
		if err != nil {
			return nil, err
		}
		u32s, err := r.readU32s(typ, cnt, val)
		if err != nil {
			return nil, err
		}
		switch tag {
		case tagWidth:
			r.width = int(first32(u32s, u16s))
		case tagHeight:
			r.height = int(first32(u32s, u16s))
		case tagBitsPerSample:
			bps = u16s
		case tagCompression:
			compression = first16(u16s)
		case tagPhotometric:
			photometric = first16(u16s)
		case tagOrientation:
			r.orient = int(first16(u16s))
		case tagStripOffsets:
			r.offsets = toU64(u32s, u16s)
		case tagSamplesPerPix:
			r.samples = int(first16(u16s))
		case tagRowsPerStrip:
			rowsPer = first32(u32s, u16s)
		case tagStripByteCnts:
			r.counts = toU64(u32s, u16s)
		case tagPlanarConfig:
			planar = first16(u16s)
		case tagExtraSamples:
			extra = u16s
			hasExtra = true
		}
	}
	if err := r.validate(compression, photometric, planar, bps, extra, hasExtra, rowsPer); err != nil {
		return nil, err
	}
	return r, nil
}

func first16(v []uint16) uint16 {
	if len(v) == 0 {
		return 0
	}
	return v[0]
}

func first32(u32 []uint32, u16 []uint16) uint32 {
	if len(u32) > 0 {
		return u32[0]
	}
	if len(u16) > 0 {
		return uint32(u16[0])
	}
	return 0
}

func toU64(u32 []uint32, u16 []uint16) []uint64 {
	var out []uint64
	if len(u32) > 0 {
		out = make([]uint64, len(u32))
		for i, v := range u32 {
			out[i] = uint64(v)
		}
		return out
	}
	out = make([]uint64, len(u16))
	for i, v := range u16 {
		out[i] = uint64(v)
	}
	return out
}

// readU16s 读取 SHORT 类型数组；内联或按偏移读取。
func (r *Reader) readU16s(typ uint16, cnt uint32, val []byte) ([]uint16, error) {
	if typ != 3 {
		return nil, nil
	}
	n := int(cnt)
	if n*2 <= 4 {
		out := make([]uint16, n)
		for i := 0; i < n; i++ {
			out[i] = r.order.Uint16(val[i*2:])
		}
		return out, nil
	}
	off := int64(r.order.Uint32(val))
	buf := make([]byte, n*2)
	if _, err := r.ra.ReadAt(buf, off); err != nil {
		return nil, fmt.Errorf("tiffthumb: 读取 SHORT 数组失败: %w", err)
	}
	out := make([]uint16, n)
	for i := 0; i < n; i++ {
		out[i] = r.order.Uint16(buf[i*2:])
	}
	return out, nil
}

// readU32s 读取 LONG 类型数组。
func (r *Reader) readU32s(typ uint16, cnt uint32, val []byte) ([]uint32, error) {
	if typ != 4 {
		return nil, nil
	}
	n := int(cnt)
	if n == 1 {
		return []uint32{r.order.Uint32(val)}, nil
	}
	off := int64(r.order.Uint32(val))
	buf := make([]byte, n*4)
	if _, err := r.ra.ReadAt(buf, off); err != nil {
		return nil, fmt.Errorf("tiffthumb: 读取 LONG 数组失败: %w", err)
	}
	out := make([]uint32, n)
	for i := 0; i < n; i++ {
		out[i] = r.order.Uint32(buf[i*4:])
	}
	return out, nil
}

func (r *Reader) validate(compression, photometric, planar uint16, bps, extra []uint16, hasExtra bool, rowsPer uint32) error {
	if r.width <= 0 || r.height <= 0 {
		return fmt.Errorf("tiffthumb: 非法图像尺寸 %dx%d", r.width, r.height)
	}
	if compression != 1 {
		return fmt.Errorf("tiffthumb: 仅支持无压缩 TIFF (compression=%d)", compression)
	}
	if photometric != 2 {
		return fmt.Errorf("tiffthumb: 仅支持 RGB 光度解释 (photometric=%d)", photometric)
	}
	if planar != 1 {
		return fmt.Errorf("tiffthumb: 仅支持交错存储 (planar=%d)", planar)
	}
	if r.samples != 3 && r.samples != 4 {
		return fmt.Errorf("tiffthumb: 仅支持 RGB/RGBA (samples=%d)", r.samples)
	}
	for _, b := range bps {
		if b != 8 {
			return fmt.Errorf("tiffthumb: 仅支持 8 位通道 (bits=%d)", b)
		}
	}
	if r.samples == 4 && hasExtra {
		if len(extra) != 1 || extra[0] != 2 {
			return fmt.Errorf("tiffthumb: 仅支持非预乘 Alpha (extra=%v)", extra)
		}
	}
	if rowsPer == 0 {
		return fmt.Errorf("tiffthumb: 缺少 RowsPerStrip")
	}
	r.rowsPer = int(rowsPer)
	nStrips := (r.height + r.rowsPer - 1) / r.rowsPer
	if len(r.offsets) < nStrips {
		return fmt.Errorf("tiffthumb: 条带偏移数量不足: %d < %d", len(r.offsets), nStrips)
	}
	r.offsets = r.offsets[:nStrips]
	if len(r.counts) < nStrips {
		// 字节计数缺失时按行字节数推导（最后一条带可能不足）。
		r.counts = make([]uint64, nStrips)
	}
	r.counts = r.counts[:nStrips]
	r.rowBytes = uint64(r.width) * uint64(r.samples)
	return nil
}

// ReadRow 将源图像第 y 行（未按取向还原）读入 buf，buf 长度须 >= width*samples。
func (r *Reader) ReadRow(y int, buf []byte) error {
	if y < 0 || y >= r.height {
		return fmt.Errorf("tiffthumb: 行号越界 %d", y)
	}
	strip := y / r.rowsPer
	rowInStrip := uint64(y % r.rowsPer)
	off := r.offsets[strip] + rowInStrip*r.rowBytes
	need := int(r.rowBytes)
	if len(buf) < need {
		return fmt.Errorf("tiffthumb: 行缓冲区过小")
	}
	if _, err := r.ra.ReadAt(buf[:need], int64(off)); err != nil {
		return fmt.Errorf("tiffthumb: 读取第 %d 行失败: %w", y, err)
	}
	return nil
}
