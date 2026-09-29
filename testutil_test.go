package tiffthumb

import (
	"encoding/binary"
	"fmt"
	"os"
	"testing"
)

// tiffSpec 描述要生成的测试 TIFF。
type tiffSpec struct {
	width, height int
	spp           int // 3 或 4
	bigEndian     bool
	orientation   int                               // 0 表示不写 Orientation 标签（缺省 1）
	rowsPerStrip  int                               // 0 表示整个图像一个条带
	stripOrder    []int                             // 物理写入顺序：第 k 个物理位置存放条带 stripOrder[k]；nil 表示顺序
	pixel         func(x, y int) (r, g, b, a uint8) // 源坐标
}

// writeTIFF 生成一个合法的经典 TIFF（无压缩、交错、8 位）。
func writeTIFF(t *testing.T, path string, spec tiffSpec) {
	t.Helper()
	if spec.rowsPerStrip <= 0 {
		spec.rowsPerStrip = spec.height
	}
	rps := spec.rowsPerStrip
	nStrips := (spec.height + rps - 1) / rps
	order := spec.stripOrder
	if order == nil {
		order = make([]int, nStrips)
		for i := range order {
			order[i] = i
		}
	}
	if len(order) != nStrips {
		t.Fatalf("stripOrder length %d != nStrips %d", len(order), nStrips)
	}

	var bo binary.ByteOrder = binary.LittleEndian
	if spec.bigEndian {
		bo = binary.BigEndian
	}

	type entry struct {
		tag  uint16
		typ  uint16
		vals []uint32
	}
	entries := []entry{
		{tagImageWidth, typeLong, []uint32{uint32(spec.width)}},
		{tagImageLength, typeLong, []uint32{uint32(spec.height)}},
	}
	bps := make([]uint32, spec.spp)
	for i := range bps {
		bps[i] = 8
	}
	entries = append(entries,
		entry{tagBitsPerSample, typeShort, bps},
		entry{tagCompression, typeShort, []uint32{1}},
		entry{tagPhotometric, typeShort, []uint32{2}},
	)
	// StripOffsets 占位，稍后回填。
	entries = append(entries, entry{tagStripOffsets, typeLong, make([]uint32, nStrips)})
	if spec.orientation != 0 {
		entries = append(entries, entry{tagOrientation, typeShort, []uint32{uint32(spec.orientation)}})
	}
	entries = append(entries,
		entry{tagSamplesPerPixel, typeShort, []uint32{uint32(spec.spp)}},
		entry{tagRowsPerStrip, typeLong, []uint32{uint32(rps)}},
	)
	byteCounts := make([]uint32, nStrips)
	for i := 0; i < nStrips; i++ {
		rows := min(rps, spec.height-i*rps)
		byteCounts[i] = uint32(rows * spec.width * spec.spp)
	}
	entries = append(entries,
		entry{tagStripByteCounts, typeLong, byteCounts},
		entry{tagPlanarConfig, typeShort, []uint32{1}},
	)
	if spec.spp == 4 {
		entries = append(entries, entry{tagExtraSamples, typeShort, []uint32{2}})
	}
	// 按标签排序（TIFF 惯例）。
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j-1].tag > entries[j].tag; j-- {
			entries[j-1], entries[j] = entries[j], entries[j-1]
		}
	}

	typeSize := func(typ uint16) int {
		if typ == typeShort {
			return 2
		}
		return 4
	}

	// 布局：8 字节头 + IFD，随后是放不下的值，再是条带数据。
	ifdSize := 2 + len(entries)*12 + 4
	dataOff := 8 + ifdSize
	extra := []byte{}
	entryBytes := make([]byte, len(entries)*12)
	stripOffsetsPos := -1
	for i, e := range entries {
		eb := entryBytes[i*12:]
		bo.PutUint16(eb[0:2], e.tag)
		bo.PutUint16(eb[2:4], e.typ)
		bo.PutUint32(eb[4:8], uint32(len(e.vals)))
		sz := typeSize(e.typ) * len(e.vals)
		putVals := func(dst []byte) {
			for j, v := range e.vals {
				if e.typ == typeShort {
					bo.PutUint16(dst[j*2:], uint16(v))
				} else {
					bo.PutUint32(dst[j*4:], v)
				}
			}
		}
		if sz <= 4 {
			putVals(eb[8:12])
		} else {
			bo.PutUint32(eb[8:12], uint32(dataOff+len(extra)))
			if e.tag == tagStripOffsets {
				stripOffsetsPos = dataOff + len(extra)
			}
			tmp := make([]byte, sz)
			putVals(tmp)
			extra = append(extra, tmp...)
		}
	}
	stripDataOff := dataOff + len(extra)

	// 计算每个条带的物理偏移（按 order 顺序排布）。
	stripOffsets := make([]uint32, nStrips)
	off := stripDataOff
	for _, si := range order {
		stripOffsets[si] = uint32(off)
		off += int(byteCounts[si])
	}
	// 回填 StripOffsets 值。
	{
		tmp := make([]byte, 4*nStrips)
		for i, v := range stripOffsets {
			bo.PutUint32(tmp[i*4:], v)
		}
		if nStrips == 1 {
			// 单条带时 StripOffsets 是内联标量。
			for i, e := range entries {
				if e.tag == tagStripOffsets {
					copy(entryBytes[i*12+8:i*12+12], tmp[:4])
				}
			}
		} else {
			copy(extra[stripOffsetsPos-dataOff:], tmp)
		}
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	hdr := make([]byte, 8)
	if spec.bigEndian {
		copy(hdr, "MM")
		bo.PutUint16(hdr[2:4], 42)
	} else {
		copy(hdr, "II")
		bo.PutUint16(hdr[2:4], 42)
	}
	bo.PutUint32(hdr[4:8], 8)
	if _, err := f.Write(hdr); err != nil {
		t.Fatal(err)
	}
	cnt := make([]byte, 2)
	bo.PutUint16(cnt, uint16(len(entries)))
	if _, err := f.Write(cnt); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(entryBytes); err != nil {
		t.Fatal(err)
	}
	next := make([]byte, 4) // 无下一个 IFD
	if _, err := f.Write(next); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(extra); err != nil {
		t.Fatal(err)
	}
	// 按物理顺序写条带数据。
	row := make([]byte, spec.width*spec.spp)
	for _, si := range order {
		rowStart := si * rps
		rowEnd := min(rowStart+rps, spec.height)
		for y := rowStart; y < rowEnd; y++ {
			for x := 0; x < spec.width; x++ {
				r, g, b, a := spec.pixel(x, y)
				p := row[x*spec.spp:]
				p[0], p[1], p[2] = r, g, b
				if spec.spp == 4 {
					p[3] = a
				}
			}
			if _, err := f.Write(row); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := f.Stat(); err != nil {
		t.Fatal(err)
	}
}

// mustInfo 读取显示信息，失败即终止测试。
func mustInfo(t *testing.T, path string) Info {
	t.Helper()
	info, err := ReadInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// generatePNGs 调用公开 API 生成三档 PNG 并解码为像素。
func generatePNGs(t *testing.T, src string) (Info, [3][]byte) {
	t.Helper()
	var paths [3]string
	for i, n := range Sizes {
		paths[i] = fmt.Sprintf("%s/thumb_%d.png", t.TempDir(), n)
	}
	if err := GenerateThumbnails(src, paths); err != nil {
		t.Fatal(err)
	}
	var out [3][]byte
	for i, p := range paths {
		out[i] = decodePNGFile(t, p)
	}
	return mustInfo(t, src), out
}
