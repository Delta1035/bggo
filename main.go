package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"bggo/internal/library"
	"bggo/internal/vscode"
	"bggo/internal/wallpaper"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

const AppVersion = "v0.2.3"

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
		// 左侧侧边栏 (Sidebar: 对齐设计图 260px，背景 t.Surface，内边距 20, 16)
		ui.Column(c).Width(260).FillHeight().Padding(20, 16).Gap(18).Background(t.Surface).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
			// 1. 顶部应用品牌与版本号徽章 (Squircle 图标 + BGGo + Version Capsule)
			ui.Row(c).AlignItems(ui.Center).FillWidth().Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Box(c).Width(26).Height(26).Radius(6).Background(t.Accent).Center().Children(func() {
						ui.Text(c, "🖼").FontSize(14).TextColor(t.AccentText)
					})
					ui.Text(c, "BGGo").FontSize(18).Bold().TextColor(t.Text)
				})
				ui.Spacer(c)
				ui.Box(c).Padding(2, 7).Radius(10).Background(t.Border).Children(func() {
					ui.Text(c, AppVersion).FontSize(11).Bold().TextColor(t.TextMuted)
				})
			})

			// 2. 选择壁纸主操作按钮 (高 36，圆角 8)
			if ui.PrimaryButton(c, "📁  选择本地图片").FillWidth().Height(36).Radius(8).Clicked() {
				a.chooseWallpaper()
			}

			// 3. 最近使用列表 (卡片化 Item 风格)
			ui.Column(c).Grow(1).Gap(6).FillWidth().Children(func() {
				ui.Text(c, "最近使用").FontSize(11).Bold().TextColor(t.TextMuted)
				ui.Scroll(c).Grow(1).Gap(4).Children(func() {
					if len(a.recent) == 0 {
						ui.Text(c, "还没有使用过壁纸").FontSize(12).TextColor(t.TextMuted).Margin(6, 0)
					}
					for _, path := range a.recent {
						path := path
						isSelected := a.selected == path
						btn := ui.Button(c.Key(path), "🖼  "+filepath.Base(path)).FillWidth().Radius(6).Padding(6, 8).Tooltip(path)
						if isSelected {
							btn.Background(t.Accent).TextColor(t.AccentText)
						} else {
							btn.Background(t.Surface).TextColor(t.Text)
						}
						if btn.Clicked() {
							a.loadWallpaper(path)
						}
					}
				})
			})

			// 4. 侧边栏底部：检查更新与版本标识 (对齐设计图 Footer)
			ui.Column(c).Gap(10).FillWidth().Children(func() {
				if ui.Button(c, "↻  检查更新").FillWidth().Height(32).Radius(6).Clicked() {
					a.checkForUpdates()
				}
				ui.Row(c).Justify(ui.Center).FillWidth().Children(func() {
					ui.Text(c, "BGGo 桌面版 · "+AppVersion).FontSize(11).TextColor(t.TextMuted)
				})
			})
		})

		// 右侧主展示区 (Main Area: 内边距 18, 24, 20, 24，Gap 16)
		ui.Column(c).Grow(1).FillHeight().Padding(18, 24, 20, 24).Gap(16).Children(func() {
			// 1. 顶部栏：标题、路径与 Segmented Control 胶囊分段选择器
			ui.Row(c).AlignItems(ui.Center).FillWidth().Children(func() {
				ui.Column(c).Gap(2).Grow(1).Children(func() {
					ui.Text(c, "桌面壁纸").FontSize(22).Bold().TextColor(t.Text)
					ui.Text(c, selectedLabel(a.selected)).FontSize(12).TextColor(t.TextMuted).MaxWidth(480).SingleLine()
				})
				ui.Spacer(c)

				// 原生胶囊分段选择器 [ 覆盖 | 适应 | 拉伸 ]
				ui.Box(c).Radius(8).Padding(3).Background(t.Border).Children(func() {
					ui.Row(c).Gap(2).Children(func() {
						options := []string{"覆盖", "适应", "拉伸"}
						for _, opt := range options {
							opt := opt
							isSelected := a.fit == opt
							btn := ui.Button(c.Key("seg-"+opt), opt).Padding(4, 12).Radius(6)
							if isSelected {
								btn.Background(t.Surface).TextColor(t.Text).Bold()
							} else {
								btn.Background(t.Border).TextColor(t.TextMuted)
							}
							if btn.Clicked() {
								a.fit = opt
							}
						}
					})
				})
			})

			// 2. 中央大图预览卡片 (带悬浮 Badge 与分辨率 Info Pill)
			ui.Box(c).Grow(1).FillWidth().MinHeight(260).Radius(14).Clip().Background(t.Surface).Border(1, t.Border).Children(func() {
				if a.preview == nil {
					ui.Column(c).Fill().Center().Gap(8).Children(func() {
						ui.Text(c, "还没有选择壁纸").FontSize(18).Bold().TextColor(t.TextMuted)
						ui.Text(c, "支持 JPG、PNG、WebP 和 BMP").FontSize(12).TextColor(t.TextMuted)
					})
					return
				}

				// 图片展示与顶部浮动信息层
				ui.Image(c, a.preview).Fill().Fit(previewFit(a.fit)).Label("所选壁纸预览")

				w, h := a.preview.Size()
				resolutionText := fmt.Sprintf("%d × %d", w, h)

				ui.Row(c).Padding(12, 14).FillWidth().Children(func() {
					// 左上角悬浮胶囊：当前壁纸
					ui.Box(c).Padding(3, 9).Radius(12).Background(t.Surface).Border(1, t.Border).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
							ui.Box(c).Width(6).Height(6).Radius(3).Background(t.Success)
							ui.Text(c, "当前壁纸").FontSize(11).Bold().TextColor(t.Text)
						})
					})
					ui.Spacer(c)
					// 右上角悬浮胶囊：分辨率尺寸
					ui.Box(c).Padding(3, 9).Radius(12).Background(t.Surface).Border(1, t.Border).Children(func() {
						ui.Text(c, resolutionText).FontSize(11).TextColor(t.TextMuted)
					})
				})
			})

			// 3. 底部操作栏 (状态指示灯、VS Code 开关、操作按钮组)
			ui.Row(c).AlignItems(ui.Center).Gap(12).FillWidth().Children(func() {
				// 状态指示灯 (Status Dot: 绿色就绪/蓝色忙碌/红色错误)
				dotColor := t.Success
				if a.error {
					dotColor = t.Danger
				} else if a.busy {
					dotColor = t.Accent
				}
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Box(c).Width(8).Height(8).Radius(4).Background(dotColor)
					ui.Text(c, a.status).FontSize(12).TextColor(t.TextMuted).SingleLine()
				})

				ui.Spacer(c)

				// 现代原生 Switch 开关
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					if ui.Switch(c, &a.syncVSCode).Changed() {
						a.toggleSyncVSCode()
					}
					ui.Text(c, "同步到 VS Code").FontSize(12).TextColor(t.Text)
				})

				if ui.Button(c, "在文件夹中显示").Height(36).Radius(8).Disabled(a.selected == "").Clicked() {
					mygo.Shell.ShowItemInFolder(a.selected)
				}
				if ui.Button(c, "映射到 VS Code").Height(36).Radius(8).Disabled(a.selected == "" || a.busy).Clicked() {
					a.applyVSCode()
				}
				label := "应用到桌面"
				if a.busy {
					label = "正在应用…"
				}
				if ui.PrimaryButton(c, label).Height(36).Radius(8).Disabled(a.selected == "" || a.busy).Clicked() {
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
		if err != nil {
			if a.window != nil {
				a.window.Update(func() {
					a.busy = false
					a.setStatus("映射到 VS Code 失败: "+err.Error(), true)
				})
			}
			return
		}
		if !report.ExtensionInstalled {
			if a.window != nil {
				a.window.Update(func() {
					a.setStatus("未检测到 BGGo 插件，正在自动下载并安装…", false)
				})
			}
			installErr := a.vscode.DownloadAndInstallExtension(context.Background())
			if a.window != nil {
				a.window.Update(func() {
					a.busy = false
					if installErr != nil {
						a.setStatus("已写入配置，但自动安装插件失败: "+installErr.Error()+"（可在 VS Code 手动安装）", true)
					} else {
						a.setStatus("已自动安装 VS Code 插件并映射背景（在 VS Code 中重新加载生效）", false)
					}
				})
			}
			return
		}
		if a.window != nil {
			a.window.Update(func() {
				a.busy = false
				a.setStatus("已映射到 VS Code 背景（在 VS Code 中重新加载生效）", false)
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
						go func() {
							_ = a.vscode.DownloadAndInstallExtension(context.Background())
						}()
						a.setStatus("壁纸已应用，正在后台自动安装 VS Code 插件…", false)
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
		if runtime.GOOS == "darwin" {
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
		} else {
			mygo.App.SetMenu(nil)
		}

		a.window = mygo.NewWindow(mygo.WindowOptions{
			Title:     "BGGo 壁纸",
			Width:     1020,
			Height:    690,
			MinWidth:  760,
			MinHeight: 520,
			StateKey:  "main",
			Content:   ui.View(a.view),
		})
		a.window.SetMenu(nil)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(fmt.Errorf("启动 BGGo: %w", err))
	}
}
