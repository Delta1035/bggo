// Command genicon creates the BGGo application icon using only the Go standard library.
//
// The mark is deliberately different from the blue Lucide line icons used in
// the UI: an indigo-to-violet tile carrying three overlapping wallpaper cards,
// with a small sun-and-hill scene on the front card. Layered cards read as
// "wallpaper collection / switching" even at tray size. The same geometry is
// mirrored in resources/icon.svg.
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
)

const (
	size   = 1024
	tileR  = 224
	cardR  = 56
	cardW  = 520
	cardH  = 380
	stepXY = 120
)

// Three landscape cards offset down-left / up-right.
var (
	backCard  = rect{140, 350, 140 + cardW, 350 + cardH}
	midCard   = rect{140 + stepXY, 350 - stepXY, 140 + stepXY + cardW, 350 - stepXY + cardH}
	frontCard = rect{140 + 2*stepXY, 350 - 2*stepXY, 140 + 2*stepXY + cardW, 350 - 2*stepXY + cardH}
)

// Sun and hills drawn on the front card.
var (
	sunX, sunY, sunR = 748, 300, 44
	hills            = [][2]int{
		{410, 540}, {540, 408}, {640, 500}, {755, 382}, {890, 520}, {890, 585}, {410, 585},
	}
)

var (
	gradStart = color.RGBA{67, 56, 202, 255}  // #4338CA indigo
	gradEnd   = color.RGBA{124, 58, 237, 255} // #7C3AED violet
	cardInk   = color.RGBA{109, 74, 222, 255} // violet artwork on white card
	white     = color.RGBA{255, 255, 255, 255}
)

type rect struct{ left, top, right, bottom int }

func main() {
	output := "resources/icon.png"
	if len(os.Args) > 1 {
		output = os.Args[1]
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// 1. Indigo-to-violet rounded tile.
	paintGradient(img, func(x, y int) bool { return rounded(x, y, 0, 0, size, size, tileR) })

	// 2. Stacked cards: translucent back, stronger mid, solid front.
	paintAlpha(img, func(x, y int) bool { return roundedRect(x, y, backCard, cardR) }, color.RGBA{255, 255, 255, 105})
	paintAlpha(img, func(x, y int) bool { return roundedRect(x, y, midCard, cardR) }, color.RGBA{255, 255, 255, 175})
	paint(img, func(x, y int) bool { return roundedRect(x, y, frontCard, cardR) }, white)

	// 3. A minimal scene clipped to the front card.
	front := func(x, y int) bool { return roundedRect(x, y, frontCard, cardR) }
	paint(img, func(x, y int) bool { return front(x, y) && circle(x, y, sunX, sunY, sunR) }, cardInk)
	paint(img, func(x, y int) bool { return front(x, y) && inPolygon(x, y, hills) }, cardInk)

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

// paintAlpha composites a translucent color over the current pixel.
func paintAlpha(img *image.RGBA, inside func(int, int) bool, c color.RGBA) {
	alpha := float64(c.A) / 255
	for y := range size {
		for x := range size {
			if !inside(x, y) {
				continue
			}
			dst := img.RGBAAt(x, y)
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(float64(c.R)*alpha + float64(dst.R)*(1-alpha)),
				G: uint8(float64(c.G)*alpha + float64(dst.G)*(1-alpha)),
				B: uint8(float64(c.B)*alpha + float64(dst.B)*(1-alpha)),
				A: 255,
			})
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
	return roundedRect(x, y, rect{left, top, right, bottom}, radius)
}

func roundedRect(x, y int, r rect, radius int) bool {
	if x < r.left || x >= r.right || y < r.top || y >= r.bottom {
		return false
	}
	cx := min(max(x, r.left+radius), r.right-radius-1)
	cy := min(max(y, r.top+radius), r.bottom-radius-1)
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
