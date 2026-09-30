package tiffthumb

// Category 表示显示图像的横纵分类。
type Category int

const (
	Square Category = iota
	Landscape
	Portrait
)

func (c Category) String() string {
	switch c {
	case Landscape:
		return "landscape"
	case Portrait:
		return "portrait"
	default:
		return "square"
	}
}

// DisplayInfo 为还原取向后的显示信息。
type DisplayInfo struct {
	Width    int
	Height   int
	Category Category
}

// displayDims 返回还原取向后的宽高；取向 5-8 交换宽高。
func displayDims(w, h, orient int) (int, int) {
	if orient >= 5 && orient <= 8 {
		return h, w
	}
	return w, h
}

func categorize(w, h int) Category {
	switch {
	case w > h:
		return Landscape
	case w < h:
		return Portrait
	default:
		return Square
	}
}

// displayCoords 将源像素坐标 (sx,sy) 映射为显示坐标 (dx,dy)。
// w,h 为源图存储尺寸。
func displayCoords(sx, sy, w, h, orient int) (int, int) {
	switch orient {
	case 2: // 水平镜像
		return w - 1 - sx, sy
	case 3: // 旋转 180
		return w - 1 - sx, h - 1 - sy
	case 4: // 垂直镜像
		return sx, h - 1 - sy
	case 5: // 主对角线转置
		return sy, sx
	case 6: // 顺时针 90
		return h - 1 - sy, sx
	case 7: // 副对角线转置
		return h - 1 - sy, w - 1 - sx
	case 8: // 逆时针 90
		return sy, w - 1 - sx
	default: // 1 原样
		return sx, sy
	}
}
