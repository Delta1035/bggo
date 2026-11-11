package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"bggo/internal/library"
	"bggo/internal/vscode"
	"bggo/internal/wallpaper"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

const AppVersion = "v0.2.1"

type application struct {
	window      *mygo.Window
	store       *library.Store
	setter      wallpaper.Service
	vscode      vscode.Service
	recent      []string
	selected    string
	preview     *ui.Bitmap
	fit         string
	syncVSCode  bool
	busy        bool
	status      string
	error       bool
	checkUpdate func()
}

func newApplication() *application {
	a := &application{
		fit:         "覆盖",
		status:      "选择一张图片开始",
		checkUpdate: updater.CheckForUpdates,
	}
	store, err := library.NewStore()
	if err != nil {
		a.setStatus("无法打开壁纸库: "+err.Error(), true)
		return a
	}
	a.store = store
	state, err := store.Load()
	if err != nil {
		a.setStatus("无法读取最近壁纸: "+err.Error(), true)
		return a
	}
	a.syncVSCode = state.SyncVSCode
	for _, path := range state.Recent {
		if _, err := os.Stat(path); err == nil {
			a.recent = append(a.recent, path)
		}
	}
	return a
}

func (a *application) view(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Fill().Background(t.Background).Children(func() {
		// 左侧侧边栏 (Sidebar)
		ui.Column(c).Width(250).FillHeight().Padding(20, 16).Gap(16).Background(t.Surface).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
			// 1. 顶部应用品牌与版本号徽章 (符合 Windows & Linux 规范的标准桌面设计，无假路灯)
			ui.Row(c).AlignItems(ui.Center).FillWidth().Children(func() {
				ui.Text(c, "BGGo").FontSize(22).Bold().TextColor(t.Text)
				ui.Spacer(c)
				ui.Box(c).Padding(2, 7).Radius(6).Background(t.Border).Children(func() {
					ui.Text(c, AppVersion).FontSize(11).Bold().TextColor(t.TextMuted)
				})
			})

			// 2. 选择壁纸主操作按钮
			if ui.PrimaryButton(c, "选择图片").FillWidth().Clicked() {
				a.chooseWallpaper()
			}

			// 3. 最近使用
			ui.Text(c, "最近使用").FontSize(11).Bold().TextColor(t.TextMuted)
			ui.Scroll(c).Grow(1).Gap(4).Children(func() {
				if len(a.recent) == 0 {
					ui.Text(c, "还没有使用过壁纸").FontSize(12).TextColor(t.TextMuted).Margin(6, 0)
				}
				for _, path := range a.recent {
					path := path
					isSelected := a.selected == path
					btn := ui.Button(c.Key(path), filepath.Base(path)).FillWidth().Tooltip(path)
					if isSelected {
						btn.Background(t.Accent).TextColor(t.AccentText)
					}
					if btn.Clicked() {
						a.loadWallpaper(path)
					}
				}
			})

			// 4. 侧边栏底部：检查更新与版本提示 (彻底去掉多余的 Linux/Windows 适配文本)
			ui.Column(c).Gap(8).FillWidth().Children(func() {
				if ui.Button(c, "检查更新").FillWidth().Clicked() {
					a.checkForUpdates()
				}
				ui.Row(c).Justify(ui.Center).FillWidth().Children(func() {
					ui.Text(c, "BGGo 桌面版 · "+AppVersion).FontSize(11).TextColor(t.TextMuted)
				})
			})
		})

		// 右侧主展示区
		ui.Column(c).Grow(1).FillHeight().Padding(24, 28).Gap(16).Children(func() {
			// 1. 顶部标题与预览模式
			ui.Row(c).AlignItems(ui.Center).FillWidth().Children(func() {
				ui.Column(c).Gap(2).Grow(1).Children(func() {
					ui.Text(c, "桌面壁纸").FontSize(22).Bold().TextColor(t.Text)
					ui.Text(c, selectedLabel(a.selected)).FontSize(12).TextColor(t.TextMuted).MaxWidth(520).SingleLine()
				})
				ui.Spacer(c)
				ui.Text(c, "预览方式").FontSize(12).TextColor(t.TextMuted).Margin(0, 8, 0, 0)
				ui.Select(c, &a.fit, []string{"覆盖", "适应", "拉伸"}).Label("预览方式").Width(110)
			})

			// 2. 中央大图预览卡片
			ui.Box(c).Grow(1).FillWidth().MinHeight(260).Radius(14).Clip().Background(t.Surface).Border(1, t.Border).Children(func() {
				if a.preview == nil {
					ui.Column(c).Fill().Center().Gap(8).Children(func() {
						ui.Text(c, "还没有选择壁纸").FontSize(18).Bold().TextColor(t.TextMuted)
						ui.Text(c, "支持 JPG、PNG、WebP 和 BMP").FontSize(12).TextColor(t.TextMuted)
					})
					return
				}
				ui.Image(c, a.preview).Fill().Fit(previewFit(a.fit)).Label("所选壁纸预览")
			})

			// 3. 底部操作栏
			ui.Row(c).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
				statusColor := t.TextMuted
				if a.error {
					statusColor = t.Danger
				}
				ui.Text(c, a.status).Grow(1).FontSize(12).TextColor(statusColor).SingleLine()

				// 现代原生 Switch 开关
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					if ui.Switch(c, &a.syncVSCode).Changed() {
						a.toggleSyncVSCode()
					}
					ui.Text(c, "同步到 VS Code").FontSize(12).TextColor(t.Text)
				})

				if ui.Button(c, "在文件夹中显示").Disabled(a.selected == "").Clicked() {
					mygo.Shell.ShowItemInFolder(a.selected)
				}
				if ui.Button(c, "映射到 VS Code").Disabled(a.selected == "" || a.busy).Clicked() {
					a.applyVSCode()
				}
				label := "应用到桌面"
				if a.busy {
					label = "正在应用…"
				}
				if ui.PrimaryButton(c, label).Disabled(a.selected == "" || a.busy).Clicked() {
					a.applyWallpaper()
				}
			})
		})
	})
}

func (a *application) chooseWallpaper() {
	if a.window == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: a.window,
			Title:  "选择壁纸",
			Filters: []mygo.FileFilter{{
				Name:       "图片",
				Extensions: []string{"jpg", "jpeg", "png", "webp", "bmp"},
			}},
		})
		if err != nil {
			a.window.Update(func() { a.setStatus("选择图片失败: "+err.Error(), true) })
			return
		}
		if len(paths) > 0 {
			a.window.Update(func() { a.loadWallpaper(paths[0]) })
		}
	}()
}

func (a *application) loadWallpaper(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		a.setStatus("读取图片失败: "+err.Error(), true)
		return
	}
	preview, err := ui.DecodeBitmap(data)
	if err != nil {
		a.setStatus("无法预览这张图片: "+err.Error(), true)
		return
	}
	a.selected, a.preview = path, preview
	a.setStatus("准备应用到桌面", false)
}

func (a *application) toggleSyncVSCode() {
	if a.store != nil {
		_, _ = a.store.SetSyncVSCode(a.syncVSCode)
	}
}

func (a *application) applyVSCode() {
	path := a.selected
	if path == "" {
		return
	}
	a.busy = true
	a.setStatus("正在映射到 VS Code 背景…", false)
	go func() {
		report, err := a.vscode.Apply(path)
		if a.window != nil {
			a.window.Update(func() {
				a.busy = false
				if err != nil {
					a.setStatus("映射到 VS Code 失败: "+err.Error(), true)
					return
				}
				if !report.ExtensionInstalled {
					a.setStatus("已写入 VS Code 配置（未检测到 BGGo 插件，请在 VS Code 安装生效）", false)
				} else {
					a.setStatus("已映射到 VS Code 背景（在 VS Code 中重新加载生效）", false)
				}
			})
		}
	}()
}

func (a *application) applyWallpaper() {
	path := a.selected
	a.busy = true
	a.setStatus("正在设置系统壁纸…", false)
	syncVSCode := a.syncVSCode
	go func() {
		err := a.setter.Set(path)
		var vscodeErr error
		var vscodeReport *vscode.ApplyReport
		if err == nil && syncVSCode {
			vscodeReport, vscodeErr = a.vscode.Apply(path)
		}
		if a.window != nil {
			a.window.Update(func() {
				a.busy = false
				if err != nil {
					a.setStatus(err.Error(), true)
					return
				}
				if syncVSCode {
					if vscodeErr != nil {
						a.setStatus("壁纸已应用，但映射到 VS Code 失败: "+vscodeErr.Error(), true)
					} else if vscodeReport != nil && !vscodeReport.ExtensionInstalled {
						a.setStatus("壁纸已应用，已写入 VS Code 配置（未检测到插件）", false)
					} else {
						a.setStatus("壁纸已应用，并已映射到 VS Code 背景", false)
					}
				} else {
					a.setStatus("壁纸已应用", false)
				}
				if a.store != nil {
					if state, saveErr := a.store.Add(path); saveErr == nil {
						a.recent = state.Recent
					} else {
						a.setStatus("壁纸已应用，但保存历史记录失败: "+saveErr.Error(), true)
					}
				}
			})
		}
	}()
}

func (a *application) setStatus(message string, isError bool) {
	a.status, a.error = message, isError
}

func (a *application) checkForUpdates() {
	a.setStatus("正在检查应用更新…", false)
	if a.checkUpdate != nil {
		a.checkUpdate()
	}
}

func selectedLabel(path string) string {
	if path == "" {
		return "选择本地图片并预览"
	}
	return path
}

func previewFit(value string) ui.Fit {
	switch value {
	case "适应":
		return ui.Contain
	case "拉伸":
		return ui.FillBox
	default:
		return ui.Cover
	}
}

func main() {
	mygo.Use(native.Plugin)
	a := newApplication()
	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Label: "BGGo", Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleAbout},
				updater.MenuItem(),
				mygo.Separator(),
				{Role: mygo.RoleQuit},
			}},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleWindowMenu},
		}))
		a.window = mygo.NewWindow(mygo.WindowOptions{
			Title:     "BGGo 壁纸",
			Width:     1000,
			Height:    680,
			MinWidth:  760,
			MinHeight: 520,
			StateKey:  "main",
			Content:   ui.View(a.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(fmt.Errorf("启动 BGGo: %w", err))
	}
}
