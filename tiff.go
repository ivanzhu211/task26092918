package tiffthumb

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Shape 表示还原取向后的画面分类。
type Shape int

const (
	ShapeSquare Shape = iota
	ShapeLandscape
	ShapePortrait
)

func (s Shape) String() string {
	switch s {
	case ShapeLandscape:
		return "landscape"
	case ShapePortrait:
		return "portrait"
	default:
		return "square"
	}
}

// Info 是显示信息：尺寸为还原取向后的宽、高。
type Info struct {
	Width       int
	Height      int
	Shape       Shape
	Orientation int
}

// TIFF 标签。
const (
	tagImageWidth      = 256
	tagImageLength     = 257
	tagBitsPerSample   = 258
	tagCompression     = 259
	tagPhotometric     = 262
	tagStripOffsets    = 273
	tagOrientation     = 274
	tagSamplesPerPixel = 277
	tagRowsPerStrip    = 278
	tagStripByteCounts = 279
	tagPlanarConfig    = 284
	tagExtraSamples    = 338
)

// TIFF 字段类型。
const (
	typeShort = 3
	typeLong  = 4
)

// tiffHeader 是解析后的首个图像目录中与本库相关的字段。
type tiffHeader struct {
	width         int
	height        int
	samplesPerPx  int // 3 或 4
	orientation   int // 1..8，缺省 1
	rowsPerStrip  int
	stripOffsets  []uint32
	stripByteCnts []uint32
}

// displayWidth/Height 返回还原取向后的尺寸。
func (h *tiffHeader) displaySize() (int, int) {
	if h.orientation >= 5 {
		return h.height, h.width
	}
	return h.width, h.height
}

func (h *tiffHeader) info() Info {
	w, ht := h.displaySize()
	shape := ShapeSquare
	switch {
	case w > ht:
		shape = ShapeLandscape
	case w < ht:
		shape = ShapePortrait
	}
	return Info{Width: w, Height: ht, Shape: shape, Orientation: h.orientation}
}

// readHeader 解析经典 TIFF 的首个图像目录，并校验业务规则限定的格式子集。
func readHeader(r io.ReaderAt) (*tiffHeader, error) {
	var hdr [8]byte
	if _, err := r.ReadAt(hdr[:], 0); err != nil {
		return nil, fmt.Errorf("read tiff header: %w", err)
	}
	var bo binary.ByteOrder
	switch string(hdr[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return nil, fmt.Errorf("invalid byte order mark %q", hdr[:2])
	}
	if bo.Uint16(hdr[2:4]) != 42 {
		return nil, fmt.Errorf("not a classic tiff (magic != 42)")
	}
	ifdOff := int64(bo.Uint32(hdr[4:8]))

	var cntBuf [2]byte
	if _, err := r.ReadAt(cntBuf[:], ifdOff); err != nil {
		return nil, fmt.Errorf("read ifd count: %w", err)
	}
	entryCount := int(bo.Uint16(cntBuf[:]))
	entryBuf := make([]byte, entryCount*12)
	if _, err := r.ReadAt(entryBuf, ifdOff+2); err != nil {
		return nil, fmt.Errorf("read ifd entries: %w", err)
	}

	h := &tiffHeader{orientation: 1}
	var bitsPerSample, extraSamples []uint32
	var compression, photometric, planar uint32 = 1, 2, 1
	spp := uint32(0)

	getVals := func(tag int) ([]uint32, bool, error) {
		for i := 0; i < entryCount; i++ {
			e := entryBuf[i*12:]
			if int(bo.Uint16(e[0:2])) != tag {
				continue
			}
			typ := int(bo.Uint16(e[2:4]))
			count := int64(bo.Uint32(e[4:8]))
			var size int
			switch typ {
			case typeShort:
				size = 2
			case typeLong:
				size = 4
			default:
				return nil, false, fmt.Errorf("tag %d: unsupported field type %d", tag, typ)
			}
			total := count * int64(size)
			var raw []byte
			if total <= 4 {
				raw = e[8 : 8+total]
			} else {
				off := int64(bo.Uint32(e[8:12]))
				raw = make([]byte, total)
				if _, err := r.ReadAt(raw, off); err != nil {
					return nil, false, fmt.Errorf("tag %d: read value: %w", tag, err)
				}
			}
			vals := make([]uint32, count)
			for j := range vals {
				if typ == typeShort {
					vals[j] = uint32(bo.Uint16(raw[j*2:]))
				} else {
					vals[j] = bo.Uint32(raw[j*4:])
				}
			}
			return vals, true, nil
		}
		return nil, false, nil
	}

	scalar := func(tag int, def uint32) (uint32, error) {
		vals, ok, err := getVals(tag)
		if err != nil {
			return 0, err
		}
		if !ok || len(vals) == 0 {
			return def, nil
		}
		return vals[0], nil
	}

	var err error
	req := func(tag int) (uint32, error) {
		vals, ok, err2 := getVals(tag)
		if err2 != nil {
			return 0, err2
		}
		if !ok || len(vals) == 0 {
			return 0, fmt.Errorf("missing required tag %d", tag)
		}
		return vals[0], nil
	}

	w64, err := req(tagImageWidth)
	if err != nil {
		return nil, err
	}
	h64, err := req(tagImageLength)
	if err != nil {
		return nil, err
	}
	h.width, h.height = int(w64), int(h64)
	if h.width <= 0 || h.height <= 0 {
		return nil, fmt.Errorf("invalid dimensions %dx%d", h.width, h.height)
	}

	if bitsPerSample, _, err = getVals(tagBitsPerSample); err != nil {
		return nil, err
	}
	if compression, err = scalar(tagCompression, 1); err != nil {
		return nil, err
	}
	if photometric, err = scalar(tagPhotometric, 2); err != nil {
		return nil, err
	}
	if spp, err = scalar(tagSamplesPerPixel, 0); err != nil {
		return nil, err
	}
	if planar, err = scalar(tagPlanarConfig, 1); err != nil {
		return nil, err
	}
	orient, err := scalar(tagOrientation, 1)
	if err != nil {
		return nil, err
	}
	if extraSamples, _, err = getVals(tagExtraSamples); err != nil {
		return nil, err
	}

	if compression != 1 {
		return nil, fmt.Errorf("unsupported compression %d (need none)", compression)
	}
	if photometric != 2 {
		return nil, fmt.Errorf("unsupported photometric %d (need RGB)", photometric)
	}
	if planar != 1 {
		return nil, fmt.Errorf("unsupported planar config %d (need interleaved)", planar)
	}
	if spp != 3 && spp != 4 {
		return nil, fmt.Errorf("unsupported samples per pixel %d (need 3 or 4)", spp)
	}
	h.samplesPerPx = int(spp)
	for _, b := range bitsPerSample {
		if b != 8 {
			return nil, fmt.Errorf("unsupported bits per sample %d (need 8)", b)
		}
	}
	if len(bitsPerSample) > 0 && len(bitsPerSample) != h.samplesPerPx {
		return nil, fmt.Errorf("bits per sample count %d != samples %d", len(bitsPerSample), h.samplesPerPx)
	}
	if h.samplesPerPx == 4 && len(extraSamples) > 0 && extraSamples[0] != 2 {
		return nil, fmt.Errorf("unsupported extra sample type %d (need unassociated alpha)", extraSamples[0])
	}
	if orient < 1 || orient > 8 {
		return nil, fmt.Errorf("invalid orientation %d", orient)
	}
	h.orientation = int(orient)

	rps, err := req(tagRowsPerStrip)
	if err != nil {
		return nil, err
	}
	h.rowsPerStrip = int(rps)
	if h.rowsPerStrip <= 0 {
		return nil, fmt.Errorf("invalid rows per strip %d", h.rowsPerStrip)
	}

	offs, ok, err := getVals(tagStripOffsets)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("missing strip offsets")
	}
	cnts, ok, err := getVals(tagStripByteCounts)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("missing strip byte counts")
	}
	if len(offs) != len(cnts) {
		return nil, fmt.Errorf("strip offsets (%d) and byte counts (%d) mismatch", len(offs), len(cnts))
	}
	wantStrips := (h.height + h.rowsPerStrip - 1) / h.rowsPerStrip
	if len(offs) != wantStrips {
		return nil, fmt.Errorf("strip count %d != expected %d", len(offs), wantStrips)
	}
	h.stripOffsets, h.stripByteCnts = offs, cnts
	return h, nil
}
