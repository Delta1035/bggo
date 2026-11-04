package wallpaper

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Service validates wallpaper files before handing them to the OS adapter.
type Service struct{}

func (Service) Set(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("解析图片路径: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("读取图片: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("请选择图片文件，而不是目录")
	}
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".bmp":
	default:
		return fmt.Errorf("不支持的图片格式 %q", filepath.Ext(abs))
	}
	return setSystemWallpaper(abs)
}
