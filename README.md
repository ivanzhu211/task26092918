# tiffthumb

本地 TIFF 素材缩略图库（Go），供素材入库流程调用：读取显示信息，并生成
150×150 / 400×400 / 800×800 三档方形 PNG 缩略图。

## 支持的输入

经典 TIFF 首个图像目录：无压缩、分条带、8 位交错 RGB 或非预乘 RGBA，
大小端均可；条带物理顺序任意，末条带可不足标准行数。

## 用法

```go
info, err := tiffthumb.ReadInfo("scan.tiff")
// info.Width/Height：还原 Orientation 后的显示尺寸
// info.Shape：ShapeLandscape / ShapePortrait / ShapeSquare

err = tiffthumb.GenerateThumbnails("scan.tiff", [3]string{
    "thumb_150.png", "thumb_400.png", "thumb_800.png",
})
```

命令行示例：

```sh
go run ./cmd/tiffthumb scan.tiff thumb_150.png thumb_400.png thumb_800.png
```

## 语义

- Orientation 1–8 按 EXIF 约定还原；缺省为 1。输出 PNG 不再依赖取向元数据。
- 裁剪：显示图像正中央、边长为较短边的正方形，边界可落在半像素处。
- 重采样：精确面积加权。Alpha 为覆盖面积加权平均；RGB 为
  （面积 × 原始 Alpha）加权平均（非预乘）；RGB 原图 Alpha 视为 255。
  通道四舍五入、半值向上；输出 Alpha 为 0 时 RGB 全 0。
  全部权重以 1/(2N) 像素为单位的整数运算完成，无浮点误差，
  三档各自独立从原图计算。
- 颜色按文件中的 8 位通道值处理，不解释 ICC、不做伽马转换。

## 资源预算

流式按条带读取（4MiB 分块），内存与图像尺寸无关：三档累加器合计约 26MiB。
10000×6000 RGB 实测（Apple Silicon）：三档合计约 1.3s，峰值常驻内存约 50MiB
（预算：30s / 96MiB）。除三份结果外不产生临时文件，不修改原图。

## 测试

```sh
go test ./...          # 全部（含 10000×6000 大图测试）
go test -short ./...   # 跳过大图测试
```

- 小图：两种字节序、全部 8 种取向、半像素裁剪、全透明强色与半透明相邻、
  乱序/不足行条带；三档 PNG 与独立的 big.Rat 精确面积加权参考逐像素一致。
- 大图：整图单条带、物理乱序条带、取向 6+乱序三种布局结果逐字节一致，
  并报告耗时与峰值内存（子进程 `getrusage` 测量）。
