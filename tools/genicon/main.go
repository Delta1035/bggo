// Command genicon creates the BGGo application icon using only the Go standard library.
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
)

func main() {
	output := "resources/icon.png"
	if len(os.Args) > 1 {
		output = os.Args[1]
	}
	img := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	paint(img, func(x, y int) bool { return rounded(x, y, 0, 0, 1024, 1024, 224) }, color.RGBA{255, 244, 223, 255})
	paintGradient(img, func(x, y int) bool { return rounded(x, y, 80, 80, 944, 944, 172) })
	paint(img, func(x, y int) bool { return circle(x, y, 726, 318, 104) }, color.RGBA{255, 230, 168, 255})
	paint(img, func(x, y int) bool {
		return inPolygon(x, y, [][2]int{{80, 720}, {354, 430}, {544, 620}, {656, 504}, {944, 774}, {944, 944}, {80, 944}})
	}, color.RGBA{7, 40, 36, 255})
	paint(img, func(x, y int) bool {
		return inPolygon(x, y, [][2]int{{80, 830}, {378, 556}, {544, 722}, {664, 598}, {944, 842}, {944, 944}, {80, 944}})
	}, color.RGBA{13, 91, 79, 255})

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
	for y := range 1024 {
		for x := range 1024 {
			if inside(x, y) {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func paintGradient(img *image.RGBA, inside func(int, int) bool) {
	for y := range 1024 {
		for x := range 1024 {
			if !inside(x, y) {
				continue
			}
			t := float64(x+y-160) / 1728
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(255 - 25*t),
				G: uint8(122 - 63*t),
				B: uint8(69 - 23*t),
				A: 255,
			})
		}
	}
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
