//go:build windows

package wallpaper

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	spiSetDesktopWallpaper = 0x0014
	spifUpdateINIFile      = 0x0001
	spifSendWinINIChange   = 0x0002
)

func setSystemWallpaper(path string) error {
	value, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("转换壁纸路径: %w", err)
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("SystemParametersInfoW")
	ok, _, callErr := proc.Call(
		spiSetDesktopWallpaper,
		0,
		uintptr(unsafe.Pointer(value)),
		spifUpdateINIFile|spifSendWinINIChange,
	)
	if ok == 0 {
		return fmt.Errorf("Windows 设置壁纸失败: %w", callErr)
	}
	return nil
}
