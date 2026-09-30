// tiffthumb 是最小调用示例：读取 TIFF 显示信息并生成三档缩略图。
//
// 用法: tiffthumb <input.tiff> <输出目录>
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"tiffthumb"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "用法: %s <input.tiff> <输出目录>\n", os.Args[0])
		os.Exit(2)
	}
	src, dir := os.Args[1], os.Args[2]

	info, err := tiffthumb.ReadInfo(src)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取信息失败:", err)
		os.Exit(1)
	}
	fmt.Printf("显示尺寸: %dx%d (%s)\n", info.Width, info.Height, info.Category)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "创建输出目录失败:", err)
		os.Exit(1)
	}
	outs := make([]string, len(tiffthumb.ThumbSizes))
	for i, s := range tiffthumb.ThumbSizes {
		outs[i] = filepath.Join(dir, fmt.Sprintf("thumb_%d.png", s))
	}
	if err := tiffthumb.GenerateThumbnails(src, outs); err != nil {
		fmt.Fprintln(os.Stderr, "生成缩略图失败:", err)
		os.Exit(1)
	}
	for _, p := range outs {
		fmt.Println("已生成:", p)
	}
}
