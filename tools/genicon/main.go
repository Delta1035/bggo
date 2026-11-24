// Command genicon creates the BGGo application icon using only the Go standard library.
//
// The icon is defined once here and kept in sync with resources/icon.svg:
// a blue rounded tile (matching the in-app Breeze UI accent) carrying a white
// sun above a two-peak mountain silhouette — the universal "wallpaper" motif.
// Large, high-contrast shapes keep it legible down to tray size (16x16).
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
)

const (
	size  = 1024
	tileR = 224 // 圆角半径
	sunX  = 736
	sunY  = 288
	sunR  = 96
)

// 连绵山峰：右峰高于左峰，太阳位于右峰上方，两者不重叠。
var peaks = [][2]int{
	{96, 812}, {350, 500}, {520, 660}, {700, 440}, {936, 780}, {936, 940}, {96, 940},
}

// 蓝色主题渐变端点，与 Breeze UI 主色 (#2563EB) 对齐。
var (
	gradStart = color.RGBA{79, 140, 255, 255} // #4F8CFF
	gradEnd   = color.RGBA{37, 99, 235, 255}  // #2563EB
	white     = color.RGBA{255, 255, 255, 255}
)

func main() {
	output := "resources/icon.png"
	if len(os.Args) > 1 {
		output = os.Args[1]
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// 1. 蓝色渐变圆角底
	paintGradient(img, func(x, y int) bool { return rounded(x, y, 0, 0, size, size, tileR) })

	// 2. 白色太阳（右上）
	paint(img, func(x, y int) bool { return circle(x, y, sunX, sunY, sunR) }, white)

	// 3. 白色连绵山峰（底部）
	paint(img, func(x, y int) bool { return inPolygon(x, y, peaks) }, white)

	file, err := os.Create(output)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		log.Fatal(err)
	}
}

func paint(img *image.RGBA, inside func(int, int) bool, c color.RGBA) {
	for y := range size {
		for x := range size {
			if inside(x, y) {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func paintGradient(img *image.RGBA, inside func(int, int) bool) {
	for y := range size {
		for x := range size {
			if !inside(x, y) {
				continue
			}
			t := float64(x+y) / float64(2*size-2)
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
			img.SetRGBA(x, y, color.RGBA{
				R: lerp(gradStart.R, gradEnd.R, t),
				G: lerp(gradStart.G, gradEnd.G, t),
				B: lerp(gradStart.B, gradEnd.B, t),
				A: 255,
			})
		}
	}
}

func lerp(a, b uint8, t float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*t)
}

func rounded(x, y, left, top, right, bottom, radius int) bool {
	if x < left || x >= right || y < top || y >= bottom {
		return false
	}
	cx := min(max(x, left+radius), right-radius-1)
	cy := min(max(y, top+radius), bottom-radius-1)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= radius*radius
}

func circle(x, y, cx, cy, radius int) bool {
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= radius*radius
}

func inPolygon(x, y int, points [][2]int) bool {
	inside := false
	for i, j := 0, len(points)-1; i < len(points); i, j = i+1, i {
		xi, yi := points[i][0], points[i][1]
		xj, yj := points[j][0], points[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			inside = !inside
		}
	}
	return inside
}
