//go:build linux

package wallpaper

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

func setSystemWallpaper(path string) error {
	desktop := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP") + ":" + os.Getenv("DESKTOP_SESSION"))
	uri := (&url.URL{Scheme: "file", Path: path}).String()

	var setters []func() error
	switch {
	case strings.Contains(desktop, "gnome"), strings.Contains(desktop, "ubuntu"):
		setters = []func() error{func() error { return setGSettings("org.gnome.desktop.background", uri) }}
	case strings.Contains(desktop, "cinnamon"):
		setters = []func() error{func() error { return run("gsettings", "set", "org.cinnamon.desktop.background", "picture-uri", uri) }}
	case strings.Contains(desktop, "mate"):
		setters = []func() error{func() error { return run("gsettings", "set", "org.mate.background", "picture-filename", path) }}
	case strings.Contains(desktop, "kde"), strings.Contains(desktop, "plasma"):
		setters = []func() error{func() error { return run("plasma-apply-wallpaperimage", path) }}
	case strings.Contains(desktop, "xfce"):
		setters = []func() error{func() error { return setXFCE(path) }}
	default:
		setters = []func() error{
			func() error { return setGSettings("org.gnome.desktop.background", uri) },
			func() error { return run("plasma-apply-wallpaperimage", path) },
			func() error { return setXFCE(path) },
		}
	}

	var errs []error
	for _, set := range setters {
		if err := set(); err == nil {
			return nil
		} else {
			errs = append(errs, err)
		}
	}
	return fmt.Errorf("当前桌面环境暂不支持: %w", errors.Join(errs...))
}

func setGSettings(schema, uri string) error {
	if err := run("gsettings", "set", schema, "picture-uri", uri); err != nil {
		return err
	}
	// GNOME keeps a separate dark-mode wallpaper. Older versions simply reject this key.
	_ = run("gsettings", "set", schema, "picture-uri-dark", uri)
	return nil
}

func setXFCE(path string) error {
	out, err := exec.Command("xfconf-query", "-c", "xfce4-desktop", "-l").Output()
	if err != nil {
		return fmt.Errorf("读取 XFCE 桌面配置: %w", err)
	}
	var changed bool
	for _, property := range strings.Fields(string(out)) {
		if strings.HasSuffix(property, "/last-image") {
			if err := run("xfconf-query", "-c", "xfce4-desktop", "-p", property, "-s", path); err != nil {
				return err
			}
			changed = true
		}
	}
	if !changed {
		return fmt.Errorf("未找到 XFCE 壁纸配置项")
	}
	return nil
}

func run(name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("缺少命令 %s", name)
	}
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
