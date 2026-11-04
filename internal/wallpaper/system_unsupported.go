//go:build !linux && !windows

package wallpaper

import (
	"fmt"
	"runtime"
)

func setSystemWallpaper(string) error {
	return fmt.Errorf("暂不支持 %s 系统", runtime.GOOS)
}
