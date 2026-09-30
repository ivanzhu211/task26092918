package tiffthumb_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// tiffSpec 描述要生成的测试 TIFF。
type tiffSpec struct {
	bigEndian    bool
	width        int
	height       int
	samples      int // 3 或 4
	orient       int
	rowsPerStrip int
	data         []byte // 行主序源像素，长度 width*height*samples
	stripOrder   []int  // 条带物理写入顺序（内容为逻辑条带号）；nil 表示顺序
}

// tiffLayout 计算 TIFF 头与条带布局，返回文件头字节、各逻辑条带偏移与数据区起点。
// 条带数据应按 phys 给定的物理顺序依次追加在返回的 dataOff 处。
func tiffLayout(bigEndian bool, w, h, samples, orient, rps int, phys []int) (header []byte, counts []uint32, dataOff int) {
	var order binary.ByteOrder = binary.LittleEndian
	if bigEndian {
		order = binary.BigEndian
	}
	nStrips := (h + rps - 1) / rps
	rowBytes := w * samples

	counts = make([]uint32, nStrips)
	for i := 0; i < nStrips; i++ {
		rows := rps
		if i == nStrips-1 {
			rows = h - rps*(nStrips-1)
		}
		counts[i] = uint32(rows * rowBytes)
	}

	pack16 := func(v ...uint16) []byte {
		b := make([]byte, 2*len(v))
		for i, x := range v {
			order.PutUint16(b[i*2:], x)
		}
		return b
	}
	pack32 := func(v ...uint32) []byte {
		b := make([]byte, 4*len(v))
		for i, x := range v {
			order.PutUint32(b[i*4:], x)
		}
		return b
	}

	type entry struct {
		tag uint16
		typ uint16
		cnt uint32
		val []byte
	}
	var entries []entry
	add := func(tag, typ uint16, cnt uint32, val []byte) {
		entries = append(entries, entry{tag, typ, cnt, val})
	}
	add(256, 4, 1, pack32(uint32(w)))
	add(257, 4, 1, pack32(uint32(h)))
	add(258, 3, uint32(samples), pack16(rep16(8, samples)...))
	add(259, 3, 1, pack16(1))
	add(262, 3, 1, pack16(2))
	add(273, 4, uint32(nStrips), nil) // StripOffsets（序列化时填）
	add(274, 3, 1, pack16(uint16(orient)))
	add(277, 3, 1, pack16(uint16(samples)))
	add(278, 4, 1, pack32(uint32(rps)))
	add(279, 4, uint32(nStrips), pack32(counts...))
	add(284, 3, 1, pack16(1))
	if samples == 4 {
		add(338, 3, 1, pack16(2))
	}
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j-1].tag > entries[j].tag; j-- {
			entries[j-1], entries[j] = entries[j], entries[j-1]
		}
	}

	ifdLen := 2 + 12*len(entries) + 4
	extraOff := 8 + ifdLen
	var extra bytes.Buffer
	extraOffs := map[int]uint32{}
	for i, e := range entries {
		if len(e.val) > 4 {
			extraOffs[i] = uint32(extraOff + extra.Len())
			extra.Write(e.val)
		}
	}
	stripOffsetsPos := uint32(extraOff + extra.Len())
	if nStrips > 1 {
		extra.Write(make([]byte, 4*nStrips))
	}
	dataOff = extraOff + extra.Len()

	if phys == nil {
		phys = make([]int, nStrips)
		for i := range phys {
			phys[i] = i
		}
	}
	offsets := make([]uint32, nStrips)
	pos := dataOff
	for _, logical := range phys {
		offsets[logical] = uint32(pos)
		pos += int(counts[logical])
	}

	var buf bytes.Buffer
	if bigEndian {
		buf.Write([]byte{'M', 'M'})
	} else {
		buf.Write([]byte{'I', 'I'})
	}
	buf.Write(pack16(42))
	buf.Write(pack32(8))
	buf.Write(pack16(uint16(len(entries))))
	for i, e := range entries {
		buf.Write(pack16(e.tag))
		buf.Write(pack16(e.typ))
		buf.Write(pack32(e.cnt))
		switch {
		case e.tag == 273:
			if nStrips == 1 {
				buf.Write(pack32(offsets[0]))
			} else {
				buf.Write(pack32(stripOffsetsPos))
			}
		case len(e.val) <= 4:
			v := make([]byte, 4)
			copy(v, e.val)
			buf.Write(v)
		default:
			buf.Write(pack32(extraOffs[i]))
		}
	}
	buf.Write(pack32(0))
	for i := range entries {
		if _, ok := extraOffs[i]; ok {
			buf.Write(entries[i].val)
		}
	}
	if nStrips > 1 {
		for _, o := range offsets {
			buf.Write(pack32(o))
		}
	}
	return buf.Bytes(), counts, dataOff
}

// writeTIFF 生成一个无压缩分条带 TIFF 文件并返回路径。
func writeTIFF(t *testing.T, spec tiffSpec) string {
	t.Helper()
	rps := spec.rowsPerStrip
	if rps <= 0 {
		rps = spec.height
	}
	header, counts, _ := tiffLayout(spec.bigEndian, spec.width, spec.height,
		spec.samples, spec.orient, rps, spec.stripOrder)

	nStrips := len(counts)
	phys := spec.stripOrder
	if phys == nil {
		phys = make([]int, nStrips)
		for i := range phys {
			phys[i] = i
		}
	}
	rowBytes := spec.width * spec.samples
	var buf bytes.Buffer
	buf.Write(header)
	for _, logical := range phys {
		start := logical * rps * rowBytes
		buf.Write(spec.data[start : start+int(counts[logical])])
	}

	path := filepath.Join(t.TempDir(), "test.tiff")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func rep16(v uint16, n int) []uint16 {
	out := make([]uint16, n)
	for i := range out {
		out[i] = v
	}
	return out
}
