package main

import (
	"fmt"
	"os"

	"tiffthumb"
)

// 最小调用示例：
//
//	tiffthumb <src.tiff> <out150.png> <out400.png> <out800.png>
func main() {
	if len(os.Args) != 5 {
		fmt.Fprintf(os.Stderr, "usage: tiffthumb <src.tiff> <out150.png> <out400.png> <out800.png>\n")
		os.Exit(2)
	}
	info, err := tiffthumb.ReadInfo(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read info:", err)
		os.Exit(1)
	}
	fmt.Printf("display: %dx%d (%s), orientation=%d\n",
		info.Width, info.Height, info.Shape, info.Orientation)
	if err := tiffthumb.GenerateThumbnails(os.Args[1], [3]string{os.Args[2], os.Args[3], os.Args[4]}); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
	fmt.Println("thumbnails written")
}
