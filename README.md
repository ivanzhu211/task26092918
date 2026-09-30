# tiffthumb

本地 TIFF 素材缩略图库，供素材入库流程调用：读取显示信息并生成 150/400/800 三档方形 PNG 缩略图。

## 能力

- 支持经典 TIFF：无压缩、分条带、8 位交错 RGB / 非预乘 RGBA，兼容大小端；只处理首个图像目录。
- 按 Orientation（1-8，缺省 1）还原显示坐标，返回还原后的宽高与横向/纵向/正方形分类。
- 取显示图像正中央、边长等于较短边的正方形（边界可落在半像素处），面积加权映射到目标尺寸，允许放大。
- 精确整数面积加权：Alpha 为覆盖面积加权平均，RGB 为面积乘原始 Alpha 加权平均；四舍五入、半值向上；Alpha 为 0 时 RGB 全 0；三档各自独立从原图计算。
- 流式按行读取，整图单条带、条带物理乱序、交换宽高的取向均不额外占用内存；除三份 PNG 外不产生临时文件，不修改原图。

## 用法

```go
info, err := tiffthumb.ReadInfo("scan.tiff")
// info.Width / info.Height / info.Category（Square|Landscape|Portrait）

err = tiffthumb.GenerateThumbnails("scan.tiff", []string{
    "thumb_150.png", "thumb_400.png", "thumb_800.png",
})
```

命令行示例：

```sh
go run ./cmd/tiffthumb input.tiff 输出目录
```

## 测试

```sh
go test ./...        # 全部测试（含 10000x6000 大图性能与一致性测试）
go test -short ./... # 跳过大图测试
```

小图测试覆盖两种字节序、全部 8 种取向、奇数边长半像素裁剪、全透明强色与半透明相邻等情况，
三档 PNG 与独立的精确面积加权参考实现逐像素一致。大图测试验证整图单条带与物理乱序条带
结果一致，并报告耗时与峰值常驻内存（本机实测：三档合计约 1.4s，峰值增量约 35MiB，
预算分别为 30s 与 96MiB）。
