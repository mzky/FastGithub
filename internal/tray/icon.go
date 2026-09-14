//go:build systray

package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
)

// iconData 返回托盘图标（Windows 下为 ICO 格式）
func iconData() []byte {
	// Windows systray 需要 ICO 格式
	// 生成 32x32 PNG 并包装为 ICO
	pngData := generateIconPNG(32)
	return pngToICO(pngData, 32, 32)
}

// generateIconPNG 生成指定尺寸的折线图风格图标（PNG 格式）
func generateIconPNG(size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	center := float64(size) / 2.0
	radius := float64(size)/2.0 - 1

	// 蓝色圆形背景
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - center + 0.5
			dy := float64(y) - center + 0.5
			dist := dx*dx + dy*dy
			if dist <= radius*radius {
				t := dist / (radius * radius)
				r := uint8(25 + 50*(1-t))
				g := uint8(100 + 60*(1-t))
				b := uint8(200 + 55*(1-t))
				a := uint8(255)
				// 边缘抗锯齿
				edgeDist := radius - dist
				if edgeDist < 1.0 {
					a = uint8(255 * edgeDist)
				}
				img.SetRGBA(x, y, color.RGBA{r, g, b, a})
			}
		}
	}

	// 绘制白色上升折线
	white := color.RGBA{255, 255, 255, 255}
	points := []struct{ x, y float64 }{
		{0.1, 0.8},
		{0.25, 0.72},
		{0.4, 0.5},
		{0.55, 0.55},
		{0.7, 0.3},
		{0.9, 0.2},
	}

	padding := float64(size) * 0.2
	left := padding
	right := float64(size) - padding
	top := padding
	bottom := float64(size) - padding

	// 绘制折线（带粗细）
	for i := 0; i < len(points)-1; i++ {
		x1 := int(left + points[i].x*(right-left))
		y1 := int(top + points[i].y*(bottom-top))
		x2 := int(left + points[i+1].x*(right-left))
		y2 := int(top + points[i+1].y*(bottom-top))
		drawThickLine(img, x1, y1, x2, y2, 2, white)
	}

	// 绘制数据点
	for _, p := range points {
		px := int(left + p.x*(right-left))
		py := int(top + p.y*(bottom-top))
		drawFilledCircle(img, px, py, 2, white)
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// pngToICO 将 PNG 数据包装为 ICO 格式（Windows Vista+ 支持 PNG 格式的 ICO）
func pngToICO(pngData []byte, width, height int) []byte {
	var buf bytes.Buffer

	// ICONDIR 头部 (6 bytes)
	binary.Write(&buf, binary.LittleEndian, uint16(0))       // Reserved
	binary.Write(&buf, binary.LittleEndian, uint16(1))       // Type: 1 = icon
	binary.Write(&buf, binary.LittleEndian, uint16(1))       // Count: 1 icon

	// ICONDIRENTRY (16 bytes)
	w := byte(width)
	if width >= 256 {
		w = 0 // 0 表示 256
	}
	h := byte(height)
	if height >= 256 {
		h = 0
	}
	buf.WriteByte(w)                                    // Width
	buf.WriteByte(h)                                    // Height
	buf.WriteByte(0)                                    // ColorCount (0 = >= 256 colors)
	buf.WriteByte(0)                                    // Reserved
	binary.Write(&buf, binary.LittleEndian, uint16(1))  // Planes
	binary.Write(&buf, binary.LittleEndian, uint16(32)) // BitCount
	binary.Write(&buf, binary.LittleEndian, uint32(len(pngData))) // BytesInRes
	binary.Write(&buf, binary.LittleEndian, uint32(22))          // ImageOffset (6 + 16 = 22)

	// PNG 图像数据
	buf.Write(pngData)

	return buf.Bytes()
}

func drawThickLine(img *image.RGBA, x0, y0, x1, y1, thickness int, c color.RGBA) {
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx := 1
	if x0 >= x1 {
		sx = -1
	}
	sy := 1
	if y0 >= y1 {
		sy = -1
	}
	err := dx - dy

	for {
		// 绘制厚度
		for t := -thickness; t <= thickness; t++ {
			for s := -thickness; s <= thickness; s++ {
				if t*t+s*s <= thickness*thickness {
					px := x0 + t
					py := y0 + s
					if px >= 0 && px < img.Bounds().Dx() && py >= 0 && py < img.Bounds().Dy() {
						img.SetRGBA(px, py, c)
					}
				}
			}
		}

		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func drawFilledCircle(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r {
				px := cx + x
				py := cy + y
				if px >= 0 && px < img.Bounds().Dx() && py >= 0 && py < img.Bounds().Dy() {
					img.SetRGBA(px, py, c)
				}
			}
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Available 返回托盘是否可用
func Available() bool { return true }
