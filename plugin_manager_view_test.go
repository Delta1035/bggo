package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPluginManagerViewShowsBuiltinsAndInstallEntry(t *testing.T) {
	a := newApplication()
	a.tab = "plugins"
	tester := ui.NewTester(a.view, 1000, 680)
	for _, text := range []string{
		"插件管理",
		"壁纸插件管理",
		"上传插件包",
		"安全安装本地插件",
		"必应每日精选",
		"内置插件",
	} {
		if !tester.HasText(text) {
			t.Errorf("missing plugin manager text %q; texts = %q", text, tester.Texts())
		}
	}
}
