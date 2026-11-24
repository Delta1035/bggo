package icons

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// 本文件补充标题栏窗口控制图标与壁纸分类图标，全部沿用标准 Lucide
// 线性风格（viewBox 24x24、2px 描边、圆角端点），与 icons.go 保持一致。

const (
	// 窗口控制
	svgMinus  = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/></svg>`
	svgSquare = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="14" height="14" x="5" y="5" rx="2"/></svg>`
	svgCopy   = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/></svg>`
	svgX      = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>`

	// 壁纸分类
	svgMountain = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m8 2 4 8 3-3 5 11H4l4-8 2 2-2-10Z"/></svg>`
	svgFlower   = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="2"/><path d="M12 10c-2-3-1.5-6 0-6s2 3 0 6"/><path d="M14 12c3-2 6-1.5 6 0s-3 2-6 0"/><path d="M12 14c2 3 1.5 6 0 6s-2-3 0-6"/><path d="M10 12c-3 2-6 1.5-6 0s3-2 6 0"/></svg>`
	svgGamepad  = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 12h4m-2-2v4"/><path d="M15 13h.01M18 11h.01"/><rect x="2" y="6" width="20" height="12" rx="4"/></svg>`
	svgPaw      = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="5" r="1.5"/><circle cx="17" cy="7" r="1.5"/><circle cx="19" cy="13" r="1.5"/><path d="M12 11c-3 0-5 2.5-5 5s2 4 5 4 5-1.5 5-4-2-5-5-5Z"/></svg>`
	svgCar      = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 17h2l-1.5-6.5a2 2 0 0 0-2-1.5H6.5a2 2 0 0 0-2 1.5L3 17h2"/><path d="M5 17h14v2H5z"/><circle cx="7.5" cy="17" r="1"/><circle cx="16.5" cy="17" r="1"/></svg>`
	svgLeaf     = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 20A7 7 0 0 1 10 6c5-1 7-2 9-4 1 3 2 5 2 8 0 5.5-4.5 10-10 10Z"/><path d="M2 21c0-3 2-5 5-6"/></svg>`
	svgPalette  = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><circle cx="8" cy="9" r="1"/><circle cx="12" cy="7" r="1"/><circle cx="16" cy="9" r="1"/></svg>`
	svgStar     = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m12 2 3.1 6.3 6.9 1-5 4.9 1.2 6.9L12 17.8 5.8 21l1.2-6.9L2 9.3l6.9-1L12 2Z"/></svg>`
	svgFilm     = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M7 3v18M17 3v18M3 12h18"/></svg>`
	svgSword    = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14.5 17.5 3 6V3h3l11.5 11.5"/><path d="m13 19 6-6M17 17l4 4M20 20l2-2"/></svg>`
	svgPen      = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/></svg>`
	svgTrophy   = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 21h8M12 17v4"/><path d="M7 4h10v5a5 5 0 0 1-10 0V4Z"/><path d="M7 6H4v2a3 3 0 0 0 3 3M17 6h3v2a3 3 0 0 1-3 3"/></svg>`
)

var (
	Minus  = ui.MustParseSVG([]byte(svgMinus))
	Square = ui.MustParseSVG([]byte(svgSquare))
	Copy   = ui.MustParseSVG([]byte(svgCopy))
	X      = ui.MustParseSVG([]byte(svgX))

	Mountain = ui.MustParseSVG([]byte(svgMountain))
	Flower   = ui.MustParseSVG([]byte(svgFlower))
	Gamepad  = ui.MustParseSVG([]byte(svgGamepad))
	Paw      = ui.MustParseSVG([]byte(svgPaw))
	Car      = ui.MustParseSVG([]byte(svgCar))
	Leaf     = ui.MustParseSVG([]byte(svgLeaf))
	Palette  = ui.MustParseSVG([]byte(svgPalette))
	Star     = ui.MustParseSVG([]byte(svgStar))
	Film     = ui.MustParseSVG([]byte(svgFilm))
	Sword    = ui.MustParseSVG([]byte(svgSword))
	Pen      = ui.MustParseSVG([]byte(svgPen))
	Trophy   = ui.MustParseSVG([]byte(svgTrophy))
)

// CategoryKey 把分类名归一化为稳定的语义键，供图标查表与测试使用。
func CategoryKey(name string) string {
	switch {
	case strings.Contains(name, "必应"):
		return "bing"
	case strings.Contains(name, "4K"):
		return "4k"
	case strings.Contains(name, "风景"):
		return "landscape"
	case strings.Contains(name, "动漫"):
		return "anime"
	case strings.Contains(name, "美女") || strings.Contains(name, "爱情"):
		return "love"
	case strings.Contains(name, "游戏"):
		return "game"
	case strings.Contains(name, "萌宠") || strings.Contains(name, "动物"):
		return "pet"
	case strings.Contains(name, "汽车"):
		return "car"
	case strings.Contains(name, "清新"):
		return "fresh"
	case strings.Contains(name, "视觉") || strings.Contains(name, "炫彩"):
		return "visual"
	case strings.Contains(name, "明星"):
		return "star"
	case strings.Contains(name, "影视"):
		return "movie"
	case strings.Contains(name, "军事"):
		return "military"
	case strings.Contains(name, "文字"):
		return "text"
	case strings.Contains(name, "体育"):
		return "sports"
	default:
		return "default"
	}
}

// ForCategory 返回分类名对应的 Lucide 图标，统一替代原先的 emoji。
func ForCategory(name string) *ui.SVG {
	switch CategoryKey(name) {
	case "bing":
		return Sparkles
	case "4k":
		return Monitor
	case "landscape":
		return Mountain
	case "anime":
		return Flower
	case "love":
		return Heart
	case "game":
		return Gamepad
	case "pet":
		return Paw
	case "car":
		return Car
	case "fresh":
		return Leaf
	case "visual":
		return Palette
	case "star":
		return Star
	case "movie":
		return Film
	case "military":
		return Sword
	case "text":
		return Pen
	case "sports":
		return Trophy
	default:
		return Image
	}
}
