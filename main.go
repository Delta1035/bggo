package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"bggo/internal/icons"
	"bggo/internal/library"
	"bggo/internal/online"
	"bggo/internal/updater"
	"bggo/internal/vscode"
	"bggo/internal/wallpaper"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	breeze "github.com/yldm-tech/mygo-breeze-ui/ui/breeze"
)

//go:embed resources/icon.png
var appIconBytes []byte

const AppVersion = "v0.2.8"

type wallpaperSetter interface {
	Set(path string) error
}

type application struct {
	window      *mygo.Window
	store       *library.Store
	setter      wallpaperSetter
	vscode      vscode.Service
	recent      []string
	favorites   []string
	selected    string
	applied     string // 当前桌面已生效的壁纸路径
	preview     *ui.Bitmap
	fit         string
	syncVSCode  bool
	autoBing    bool
	busy        bool
	status      string
	error       bool
	updaterSvc  *updater.Service
	checkUpdate func()

	// 本地浏览子标签
	localSubTab string // "recent" | "favorites"

	// 在线壁纸相关
	onlineSvc        *online.Service
	tab              string // "local" | "online"
	onlineView       string // "grid" | "preview"
	searchKeyword    string // 在线壁纸搜索关键词
	filterOption     string // 筛选选项: 精选推荐/最新壁纸/最高人气
	categories       []online.Category
	selectedCategory online.Category
	wallpapers       []online.WallpaperItem
	selectedOnline   *online.WallpaperItem
	onlinePage       int
	loadingOnline    bool
	thumbBitmaps     map[string]*ui.Bitmap
	thumbLoading     map[string]bool
	recentBitmaps    map[string]*ui.Bitmap
	thumbMu          sync.Mutex

	tray     *mygo.Tray
	quitting bool
	mu       sync.RWMutex
}

func newApplication() *application {
	a := &application{
		setter:        wallpaper.Service{},
		fit:           "覆盖",
		status:        "选择一张图片开始",
		tab:           "local",
		localSubTab:   "recent",
		onlineView:    "grid",
		onlinePage:    1,
		filterOption:  "精选推荐",
		thumbBitmaps:  make(map[string]*ui.Bitmap),
		thumbLoading:  make(map[string]bool),
		recentBitmaps: make(map[string]*ui.Bitmap),
	}

	a.initUpdater()

	if onlineSvc, err := online.NewService(""); err == nil {
		a.onlineSvc = onlineSvc
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
	a.autoBing = state.AutoBing
	for _, path := range state.Recent {
		if _, err := os.Stat(path); err == nil {
			a.recent = append(a.recent, path)
		}
	}
	if len(a.recent) > 0 {
		a.applied = a.recent[0]
	}
	for _, path := range state.Favorites {
		if _, err := os.Stat(path); err == nil {
			a.favorites = append(a.favorites, path)
		}
	}
	if a.autoBing {
		go a.runAutoBingCheck()
	}
	return a
}

func (a *application) update(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.window != nil {
		a.window.Update(fn)
	} else {
		fn()
	}
}

func (a *application) Status() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *application) IsBusy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.busy
}

func (a *application) IsLoadingOnline() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.loadingOnline
}

func (a *application) Categories() []online.Category {
	a.mu.Lock()
	defer a.mu.Unlock()
	cats := make([]online.Category, len(a.categories))
	copy(cats, a.categories)
	return cats
}

func (a *application) Wallpapers() []online.WallpaperItem {
	a.mu.Lock()
	defer a.mu.Unlock()
	wps := make([]online.WallpaperItem, len(a.wallpapers))
	copy(wps, a.wallpapers)
	return wps
}

func (a *application) Selected() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.selected
}

func (a *application) Preview() *ui.Bitmap {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.preview
}

func (a *application) Favorites() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	favs := make([]string, len(a.favorites))
	copy(favs, a.favorites)
	return favs
}

func (a *application) IsFavorite(path string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, fav := range a.favorites {
		if fav == path {
			return true
		}
	}
	return false
}

func (a *application) AutoBing() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.autoBing
}

func (a *application) view(c *ui.Context) {
	breeze.Use(c)
	t := c.Theme()

	// 最外层窗口容器：在 Frameless 自绘模式下保持 1px 微边框和微圆角
	ui.Column(c).Fill().Background(t.Background).Border(1, t.Border).Children(func() {
		// 1. 完全自绘的现代标题栏 (包含应用Logo、标题、版本胶囊、全宽拖拽区以及上一版标准最小化/最大化/关闭按钮)
		a.drawTitleBar(c, t)

		// 2. 窗口主体内容区 (侧边栏 + 主内容区)
		ui.Row(c).Grow(1).FillWidth().Children(func() {
			a.drawSidebar(c, t)
			a.drawMainArea(c, t)
		})
	})
}

// drawTitleBar 绘制完全自绘的现代化全宽标题栏 (恢复上一版标准控制按钮)
func (a *application) drawTitleBar(c *ui.Context, t *ui.Theme) {
	// 标题栏：高 38px，包含拖拽区域，双击触发最大化/还原
	ui.Row(c).Height(38).FillWidth().Background(ui.Hex("#FFFFFF")).BorderWidth(0, 0, 1, 0).BorderColor(ui.Hex("#E2E4E9")).AlignItems(ui.Center).DragWindow().Children(func() {
		// 左侧应用品牌徽标 (图标 + 标题 + 版本胶囊)
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(0, 14).Children(func() {
			ui.Box(c).Width(22).Height(22).Radius(6).Background(ui.Hex("#3B82F6")).Center().Children(func() {
				ui.Icon(c, icons.Image).Size(13, 13).TextColor(ui.Hex("#FFFFFF"))
			})
			ui.Text(c, "BGGo 壁纸").FontSize(13).Bold().TextColor(ui.Hex("#0F172A"))
			ui.Box(c).Padding(2, 7).Radius(10).Background(ui.Hex("#F1F5F9")).Border(1, ui.Hex("#E2E4E9")).Center().Children(func() {
				ui.Text(c, AppVersion).FontSize(10).Bold().TextColor(ui.Hex("#64748B"))
			})
		})

		// 中间全宽留白拖拽区
		ui.Spacer(c)

		// 右侧自绘窗口控制按钮群 (最小化、最大化/还原、关闭)
		ui.Row(c).Height(38).AlignItems(ui.Center).Children(func() {
			// 1. 最小化按钮
			minBtn := ui.ButtonBase(c.Key("win-min")).Width(44).Height(38).Tooltip("最小化")
			if minBtn.Hovered() {
				minBtn.Background(ui.Hex("#F1F5F9"))
			}
			minBtn.Center().Children(func() {
				ui.Text(c, "—").FontSize(11).TextColor(ui.Hex("#64748B"))
			})
			if minBtn.Clicked() && a.window != nil {
				a.window.Minimize()
			}

			// 2. 最大化 / 还原按钮
			isMax := a.window != nil && a.window.IsMaximized()
			maxIcon := "□"
			maxTooltip := "最大化"
			if isMax {
				maxIcon = "❐"
				maxTooltip = "还原"
			}
			maxBtn := ui.ButtonBase(c.Key("win-max")).Width(44).Height(38).Tooltip(maxTooltip)
			if maxBtn.Hovered() {
				maxBtn.Background(ui.Hex("#F1F5F9"))
			}
			maxBtn.Center().Children(func() {
				ui.Text(c, maxIcon).FontSize(12).TextColor(ui.Hex("#64748B"))
			})
			if maxBtn.Clicked() && a.window != nil {
				a.window.ToggleMaximize()
			}

			// 3. 关闭按钮 (Hover 时呈现红色背景与白色文字)
			closeBtn := ui.ButtonBase(c.Key("win-close")).Width(44).Height(38).Tooltip("关闭")
			closeTextColor := ui.Hex("#64748B")
			if closeBtn.Hovered() {
				closeBtn.Background(ui.Hex("#EF4444"))
				closeTextColor = ui.Hex("#FFFFFF")
			}
			closeBtn.Center().Children(func() {
				ui.Text(c, "✕").FontSize(11).TextColor(closeTextColor)
			})
			if closeBtn.Clicked() && a.window != nil {
				if a.tray != nil {
					a.window.Hide()
				} else {
					a.window.Close()
				}
			}
		})
	})
}

// drawSidebar 绘制左侧侧边栏 (符合 Breeze UI 设计)
func (a *application) drawSidebar(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Width(260).FillHeight().Padding(16, 16, 16, 16).Gap(14).Background(ui.Hex("#F8FAFC")).BorderWidth(0, 1, 0, 0).BorderColor(ui.Hex("#E2E4E9")).Children(func() {
		// 1. 顶部模式切换胶囊 [ 本地壁纸 | 在线图库 ]
		ui.Box(c).Radius(8).Padding(3).Background(ui.Hex("#E5E7EB")).Height(34).FillWidth().Children(func() {
			ui.Row(c).Gap(3).Fill().Children(func() {
				isLocal := a.tab == "local"
				btnLocal := ui.ButtonBase(c.Key("tab-local")).Grow(1).FillHeight().Radius(6)
				if isLocal {
					btnLocal.Background(ui.Hex("#FFFFFF"))
				} else {
					btnLocal.Background(ui.Hex("#00000000"))
				}
				btnLocal.Children(func() {
					ui.Row(c).Center().Gap(6).Children(func() {
						icCol := ui.Hex("#64748B")
						if isLocal {
							icCol = ui.Hex("#0F172A")
						}
						ui.Icon(c, icons.Monitor).Size(13, 13).TextColor(icCol)
						lbl := ui.Text(c, "本地壁纸").FontSize(12).TextColor(icCol)
						if isLocal {
							lbl.Bold()
						}
					})
				})
				if btnLocal.Clicked() {
					a.tab = "local"
				}

				isOnline := a.tab == "online"
				btnOnline := ui.ButtonBase(c.Key("tab-online")).Grow(1).FillHeight().Radius(6)
				if isOnline {
					btnOnline.Background(ui.Hex("#FFFFFF"))
				} else {
					btnOnline.Background(ui.Hex("#00000000"))
				}
				btnOnline.Children(func() {
					ui.Row(c).Center().Gap(6).Children(func() {
						icCol := ui.Hex("#64748B")
						if isOnline {
							icCol = ui.Hex("#0F172A")
						}
						ui.Icon(c, icons.Globe).Size(13, 13).TextColor(icCol)
						lbl := ui.Text(c, "在线图库").FontSize(12).TextColor(icCol)
						if isOnline {
							lbl.Bold()
						}
					})
				})
				if btnOnline.Clicked() {
					a.tab = "online"
					if len(a.categories) == 0 && !a.loadingOnline {
						a.loadOnlineCategories()
					}
				}
			})
		})

		// 4. 中间内容区：根据模式展示
		if a.tab == "local" {
			// 本地模式：选择本地图片主按钮
			chooseBtn := ui.ButtonBase(c.Key("btn-choose-file")).FillWidth().Height(36).Radius(8).Background(ui.Hex("#007AFF"))
			chooseBtn.Children(func() {
				ui.Row(c).Center().Gap(7).Children(func() {
					ui.Icon(c, icons.FolderOpen).Size(15, 15).TextColor(ui.Hex("#FFFFFF"))
					ui.Text(c, "选择本地壁纸").FontSize(13).Bold().TextColor(ui.Hex("#FFFFFF"))
				})
			})
			if chooseBtn.Clicked() {
				a.chooseWallpaper()
			}
			ui.Row(c).Justify(ui.Center).AlignItems(ui.Center).FillWidth().Children(func() {
				ui.Text(c, "或直接拖放图片到窗口").FontSize(11).TextColor(ui.Hex("#94A3B8"))
			})

			// 切换：[ 最近使用 · N | 我的收藏 · M ]
			ui.Box(c).Radius(7).Padding(2).Background(ui.Hex("#E5E7EB")).Height(30).FillWidth().Children(func() {
				ui.Row(c).Gap(2).Fill().Children(func() {
					isRecent := a.localSubTab != "favorites"
					btnRecent := ui.ButtonBase(c.Key("sub-recent")).Grow(1).FillHeight().Radius(5)
					if isRecent {
						btnRecent.Background(ui.Hex("#FFFFFF"))
					} else {
						btnRecent.Background(ui.Hex("#00000000"))
					}
					btnRecent.Children(func() {
						ui.Row(c).Center().Gap(4).Children(func() {
							col := ui.Hex("#64748B")
							if isRecent {
								col = ui.Hex("#0F172A")
							}
							ui.Icon(c, icons.Clock).Size(12, 12).TextColor(col)
							t := ui.Text(c, fmt.Sprintf("最近使用 · %d", len(a.recent))).FontSize(11).TextColor(col)
							if isRecent {
								t.Bold()
							}
						})
					})
					if btnRecent.Clicked() {
						a.localSubTab = "recent"
					}

					isFav := a.localSubTab == "favorites"
					btnFav := ui.ButtonBase(c.Key("sub-fav")).Grow(1).FillHeight().Radius(5)
					if isFav {
						btnFav.Background(ui.Hex("#FFFFFF"))
					} else {
						btnFav.Background(ui.Hex("#00000000"))
					}
					btnFav.Children(func() {
						ui.Row(c).Center().Gap(4).Children(func() {
							col := ui.Hex("#64748B")
							if isFav {
								col = ui.Hex("#EF4444")
							}
							ui.Icon(c, icons.Heart).Size(12, 12).TextColor(col)
							t := ui.Text(c, fmt.Sprintf("我的收藏 · %d", len(a.favorites))).FontSize(11).TextColor(col)
							if isFav {
								t.Bold()
							}
						})
					})
					if btnFav.Clicked() {
						a.localSubTab = "favorites"
					}
				})
			})

			// 列表渲染 (最近使用 或 收藏夹)
			ui.Column(c).Grow(1).Gap(6).FillWidth().Children(func() {
				currentList := a.recent
				emptyText := "还没有使用过壁纸"
				listTitle := fmt.Sprintf("最近使用 · %d", len(a.recent))
				if a.localSubTab == "favorites" {
					currentList = a.favorites
					emptyText = "还没有收藏的壁纸\n在大图预览点击爱心可添加收藏"
					listTitle = fmt.Sprintf("我的收藏 · %d", len(a.favorites))
				}

				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).FillWidth().Padding(0, 2).Children(func() {
					ui.Text(c, listTitle).FontSize(11).Bold().TextColor(ui.Hex("#64748B"))
					if a.localSubTab != "favorites" && len(a.recent) > 0 {
						clearBtn := ui.ButtonBase(c.Key("clear-recent")).Padding(2, 6).Radius(4).Background(ui.Hex("#F1F5F9")).Border(1, ui.Hex("#E2E8F0"))
						clearBtn.Children(func() {
							ui.Row(c).AlignItems(ui.Center).Gap(3).Children(func() {
								ui.Icon(c, icons.Trash).Size(11, 11).TextColor(ui.Hex("#64748B"))
								ui.Text(c, "清空").FontSize(11).TextColor(ui.Hex("#64748B"))
							})
						})
						if clearBtn.Clicked() {
							a.clearRecent()
						}
					}
				})

				ui.Scroll(c).Grow(1).Gap(4).Children(func() {
					if len(currentList) == 0 {
						ui.Text(c, emptyText).FontSize(12).TextColor(ui.Hex("#94A3B8")).Margin(6, 0)
					}
					for _, path := range currentList {
						path := path
						isSelected := a.selected == path
						spec := a.getWallpaperSpec(path)

						cardBg := ui.Hex("#00000000")
						borderCol := ui.Hex("#00000000")
						titleCol := ui.Hex("#334155")
						specCol := ui.Hex("#94A3B8")
						if isSelected {
							cardBg = ui.Hex("#EFF6FF")
							borderCol = ui.Hex("#007AFF")
							titleCol = ui.Hex("#0F172A")
							specCol = ui.Hex("#007AFF")
						}

						cardBtn := ui.ButtonBase(c.Key("item-"+path)).FillWidth().Height(46).Radius(8).Background(cardBg).Border(1, borderCol).Padding(6, 8).Tooltip(path)
						if cardBtn.Clicked() {
							a.loadWallpaper(path)
						}
						cardBtn.Children(func() {
							ui.Row(c).AlignItems(ui.Center).Gap(8).Fill().Children(func() {
								// 1. 左侧缩略图
								thumbBmp := a.getRecentThumbnail(path)
								if thumbBmp != nil {
									ui.Image(c, thumbBmp).Width(34).Height(22).Radius(4).Fit(ui.Cover)
								} else {
									ui.Box(c).Width(34).Height(22).Radius(4).Background(ui.Hex("#1E3E62")).Center().Children(func() {
										ui.Icon(c, icons.Image).Size(12, 12).TextColor(ui.Hex("#FFFFFF"))
									})
								}

								// 2. 中间双行信息 (标题 + 规格)
								ui.Column(c).Grow(1).Gap(1).Children(func() {
									ui.Text(c, filepath.Base(path)).FontSize(12).Bold().TextColor(titleCol).SingleLine()
									ui.Text(c, spec).FontSize(10).TextColor(specCol).SingleLine()
								})

								// 3. 右侧对勾标记 (仅选中项显示)
								if isSelected {
									ui.Icon(c, icons.Check).Size(14, 14).TextColor(ui.Hex("#007AFF"))
								}
							})
						})
					}
				})
			})
		} else {
			// 在线模式：壁纸分类列表 (带有图标与数量 Badge)
			ui.Column(c).Grow(1).Gap(6).FillWidth().Children(func() {
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).FillWidth().Padding(0, 2).Children(func() {
					ui.Text(c, "壁纸分类").FontSize(11).Bold().TextColor(ui.Hex("#64748B"))
					refreshBtn := ui.ButtonBase(c.Key("refresh-cats")).Padding(2, 6).Radius(4).Background(ui.Hex("#F1F5F9")).Border(1, ui.Hex("#E2E8F0"))
					refreshBtn.Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(3).Children(func() {
							ui.Icon(c, icons.Refresh).Size(11, 11).TextColor(ui.Hex("#64748B"))
							ui.Text(c, "刷新").FontSize(11).TextColor(ui.Hex("#64748B"))
						})
					})
					if refreshBtn.Clicked() {
						a.loadOnlineCategories()
					}
				})

				ui.Scroll(c).Grow(1).Gap(4).Children(func() {
					if len(a.categories) == 0 {
						if a.loadingOnline {
							ui.Text(c, "正在加载分类…").FontSize(12).TextColor(ui.Hex("#94A3B8")).Margin(6, 0)
						} else {
							ui.Text(c, "暂无分类数据").FontSize(12).TextColor(ui.Hex("#94A3B8")).Margin(6, 0)
						}
					}
					for _, cat := range a.categories {
						cat := cat
						isSelected := a.selectedCategory.ID == cat.ID && a.selectedCategory.Source == cat.Source
						icon := categoryIcon(cat.Name)
						badgeCount := categoryBadge(cat)

						cardBg := ui.Hex("#00000000")
						borderCol := ui.Hex("#00000000")
						textCol := ui.Hex("#334155")
						if isSelected {
							cardBg = ui.Hex("#EFF6FF")
							borderCol = ui.Hex("#007AFF")
							textCol = ui.Hex("#007AFF")
						}

						btn := ui.ButtonBase(c.Key("cat-"+cat.Source+"-"+cat.ID)).FillWidth().Height(34).Radius(7).Background(cardBg).Border(1, borderCol).Padding(6, 10)
						if btn.Clicked() {
							a.onlinePage = 1
							a.selectedCategory = cat
							a.onlineView = "grid"
							a.loadOnlineWallpapers(cat, 1)
						}
						btn.Children(func() {
							ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Fill().Children(func() {
								ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
									ui.Text(c, icon).FontSize(13)
									lbl := ui.Text(c, cat.Name).FontSize(12).TextColor(textCol)
									if isSelected {
										lbl.Bold()
									}
								})
								if isSelected {
									ui.Box(c).Padding(2, 6).Radius(10).Background(ui.Hex("#007AFF20")).Children(func() {
										ui.Text(c, badgeCount).FontSize(10).Bold().TextColor(ui.Hex("#007AFF"))
									})
								} else {
									ui.Text(c, badgeCount).FontSize(11).TextColor(ui.Hex("#94A3B8"))
								}
							})
						})
					}
				})
			})
		}

		// 5. 底部信息与检查更新
		ui.Column(c).Gap(10).FillWidth().Children(func() {
			chkBtn := ui.ButtonBase(c.Key("btn-check-update")).FillWidth().Height(32).Radius(6).Background(ui.Hex("#FFFFFF")).Border(1, ui.Hex("#D1D5DB"))
			chkBtn.Children(func() {
				ui.Row(c).Center().Gap(6).Children(func() {
					ui.Icon(c, icons.Sparkles).Size(13, 13).TextColor(ui.Hex("#3B82F6"))
					ui.Text(c, "检查更新").FontSize(12).TextColor(ui.Hex("#1E293B")).Bold()
				})
			})
			if chkBtn.Clicked() {
				a.checkForUpdates()
			}
			footerNote := "BGGo Desktop · 已是最新版"
			if a.tab == "online" {
				footerNote = "BGGo Desktop · 已连接微软官方CDN"
			}
			ui.Row(c).Justify(ui.Center).FillWidth().Children(func() {
				ui.Text(c, footerNote).FontSize(11).TextColor(ui.Hex("#94A3B8"))
			})
		})
	})
}

// drawMainArea 绘制右侧主区域
func (a *application) drawMainArea(c *ui.Context, t *ui.Theme) {
	if a.tab == "online" {
		a.drawOnlineMain(c, t)
	} else {
		a.drawLocalMain(c, t)
	}
}

// drawLocalMain 绘制本地壁纸主页面
func (a *application) drawLocalMain(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).FillHeight().Padding(18, 24, 20, 24).Gap(16).Background(ui.Hex("#FFFFFF")).Children(func() {
		// 1. 顶部栏：标题、路径与 Segmented Control 胶囊分段选择器 (支持拖动窗口)
		ui.Row(c).AlignItems(ui.Center).FillWidth().DragWindow().Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, "桌面壁纸预览").FontSize(18).Bold().TextColor(ui.Hex("#0F172A"))
				ui.Text(c, selectedLabel(a.selected)).FontSize(12).TextColor(ui.Hex("#64748B")).MaxWidth(480).SingleLine()
			})
			ui.Spacer(c)

			// 原生胶囊分段选择器 [ ⬒ 覆盖 | ⬓ 适应 | ⬖ 拉伸 ]
			a.drawFitSelector(c, t)
		})

		// 2. 中央大图预览卡片 (内嵌 macOS Dock Simulator)
		a.drawLargePreviewBox(c, t)

		// 3. 底部操作栏 (原型设计 3 元素：在文件夹中显示、VS Code 同步、应用到桌面)
		a.drawLocalBottomBar(c, t)
	})
}

// drawOnlineMain 绘制在线壁纸主页面
func (a *application) drawOnlineMain(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).FillHeight().Padding(18, 24, 20, 24).Gap(16).Background(ui.Hex("#FFFFFF")).Children(func() {
		// 1. 顶部控制栏：分类名、副标题、搜索框、筛选下拉 (两端对齐，标题不截断)
		ui.Row(c).Justify(ui.SpaceBetween).AlignItems(ui.Center).FillWidth().DragWindow().Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				catName := a.selectedCategory.Name
				if catName == "" {
					catName = "必应每日精选"
				}
				sourceText := "微软必应官方精选 · 每日自动同步 · 4K 超高清原图直连"
				if a.selectedCategory.Source == "360" {
					sourceText = "360壁纸库精选 · 国内 CDN 极速直连 · 高清多分辨率适配"
				}
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					if a.onlineView == "preview" {
						backBtn := ui.ButtonBase(c.Key("back-to-grid")).Padding(3, 8).Radius(6).Background(ui.Hex("#F1F5F9")).Border(1, ui.Hex("#CBD5E1"))
						backBtn.Children(func() {
							ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
								ui.Icon(c, icons.ChevronLeft).Size(12, 12).TextColor(ui.Hex("#475569"))
								ui.Text(c, "◀ 返回列表").FontSize(11).TextColor(ui.Hex("#475569"))
							})
						})
						if backBtn.Clicked() {
							a.onlineView = "grid"
						}
					}
					ui.Text(c, "在线壁纸 · "+catName).FontSize(18).Bold().TextColor(ui.Hex("#0F172A")).SingleLine()
				})
				ui.Text(c, sourceText).FontSize(11).TextColor(ui.Hex("#64748B")).SingleLine()
			})

			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				// 搜索框 (Breeze UI 风格，宽 180px)
				breeze.SearchField(c, &a.searchKeyword).Placeholder("搜索壁纸关键词…").Width(180).Height(32)

				// 筛选下拉 (Breeze UI 风格)
				breeze.Select(c, &a.filterOption, []string{"精选推荐", "最新壁纸", "最高人气"}).Height(32)

				// 适应模式选择器 (在大图预览时显示)
				if a.onlineView == "preview" {
					a.drawFitSelector(c, t)
				}
			})
		})

		// 2. 主体展示区 (网格卡片 或 大图预览)
		if a.onlineView == "preview" {
			a.drawLargePreviewBox(c, t)
		} else {
			a.drawWallpaperGrid(c, t)
		}

		// 3. 底部操作栏 (在线分页器与状态)
		a.drawOnlineBottomBar(c, t)
	})
}

// drawWallpaperGrid 绘制网格卡片列表 (原型图设计：卡片白底、阴影、4K UHD角标、双按钮)
func (a *application) drawWallpaperGrid(c *ui.Context, t *ui.Theme) {
	ui.Box(c).Grow(1).FillWidth().MinHeight(260).Radius(12).Clip().Background(ui.Hex("#F8FAFC")).Border(1, ui.Hex("#E2E8F0")).Padding(12, 12).Children(func() {
		if a.loadingOnline && len(a.wallpapers) == 0 {
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Text(c, "正在加载在线壁纸…").FontSize(16).Bold().TextColor(t.TextMuted)
				ui.Text(c, "正在连接国内图库源，请稍候").FontSize(12).TextColor(t.TextMuted)
			})
			return
		}

		// 关键词过滤
		displayed := a.wallpapers
		if kw := strings.TrimSpace(a.searchKeyword); kw != "" {
			var filtered []online.WallpaperItem
			for _, item := range a.wallpapers {
				if strings.Contains(strings.ToLower(item.Title), strings.ToLower(kw)) {
					filtered = append(filtered, item)
				}
			}
			displayed = filtered
		}

		// 筛选排序选项联动: 精选推荐 / 最新壁纸 / 最高人气
		switch a.filterOption {
		case "最新壁纸":
			reversed := make([]online.WallpaperItem, len(displayed))
			for i, item := range displayed {
				reversed[len(displayed)-1-i] = item
			}
			displayed = reversed
		case "最高人气":
			sorted := make([]online.WallpaperItem, len(displayed))
			copy(sorted, displayed)
			sort.SliceStable(sorted, func(i, j int) bool {
				iIs4K := strings.Contains(sorted[i].Resolution, "3840") || strings.Contains(sorted[i].Resolution, "4K")
				jIs4K := strings.Contains(sorted[j].Resolution, "3840") || strings.Contains(sorted[j].Resolution, "4K")
				if iIs4K != jIs4K {
					return iIs4K
				}
				return sorted[i].Title < sorted[j].Title
			})
			displayed = sorted
		}

		if len(displayed) == 0 {
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Text(c, "暂无壁纸数据").FontSize(16).Bold().TextColor(t.TextMuted)
				if ui.PrimaryButton(c.Key("retry-load"), "重新拉取").Padding(6, 16).Radius(6).Clicked() {
					a.loadOnlineWallpapers(a.selectedCategory, a.onlinePage)
				}
			})
			return
		}

		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Grid(c).Columns(3).Gap(12).FillWidth().Children(func() {
				for _, item := range displayed {
					item := item
					isSelected := a.selectedOnline != nil && a.selectedOnline.ID == item.ID
					borderColor := ui.Hex("#E2E8F0")
					if isSelected {
						borderColor = ui.Hex("#007AFF")
					}

					ui.Column(c.Key("card-"+item.ID)).Padding(8, 8).Gap(8).Radius(10).Background(ui.Hex("#FFFFFF")).Border(1, borderColor).Children(func() {
						// 缩略图盒子 + 左上角 4K UHD 胶囊徽标
						ui.Box(c).Height(118).FillWidth().Radius(6).Clip().Children(func() {
							a.thumbMu.Lock()
							bmp := a.thumbBitmaps[item.ID]
							a.thumbMu.Unlock()

							if bmp != nil {
								ui.Image(c, bmp).Fill().Radius(6).Fit(ui.Cover)
							} else {
								a.requestThumbnail(item)
								ui.Box(c).Fill().Background(ui.Hex("#E2E8F0")).Center().Children(func() {
									ui.Text(c, "加载中…").FontSize(11).TextColor(ui.Hex("#94A3B8"))
								})
							}

							resBadge := "4K UHD"
							if item.Resolution != "" && !strings.Contains(item.Resolution, "3840") && !strings.Contains(item.Resolution, "4K") {
								resBadge = item.Resolution
							}
							// 顶层悬浮 Badge (Absolute 左上角)
							ui.Box(c).Absolute().Top(8).Left(8).Padding(2, 6).Radius(4).Background(ui.Hex("#00000070")).Children(func() {
								ui.Text(c, resBadge).FontSize(9).Bold().TextColor(ui.Hex("#FFFFFF"))
							})
						})

						// 标题单行加粗 (符合原型设计)
						ui.Row(c).AlignItems(ui.Center).FillWidth().Children(func() {
							ui.Text(c, item.Title).FontSize(12).Bold().TextColor(ui.Hex("#0F172A")).Grow(1).SingleLine()
						})

						// 卡片操作按钮行：↗ 预览 / ✓ 应用 (等宽双按钮，对齐原型)
						ui.Row(c).Gap(6).FillWidth().Children(func() {
							prevBtn := ui.ButtonBase(c.Key("btn-prev-"+item.ID)).Grow(1).Height(28).Radius(6).Background(ui.Hex("#F1F5F9")).Border(1, ui.Hex("#CBD5E1"))
							prevBtn.Children(func() {
								ui.Row(c).Center().Gap(4).Children(func() {
									ui.Icon(c, icons.Eye).Size(12, 12).TextColor(ui.Hex("#475569"))
									ui.Text(c, "↗ 预览").FontSize(11).TextColor(ui.Hex("#475569"))
								})
							})
							if prevBtn.Clicked() {
								a.selectedOnline = &item
								a.previewOnlineWallpaper(item)
							}

							btnLabel := "✓ 应用"
							if a.busy && isSelected {
								btnLabel = "正在应用…"
							}
							applyBtn := ui.ButtonBase(c.Key("btn-apply-" + item.ID)).Grow(1).Height(28).Radius(6).Disabled(a.busy)
							if a.busy {
								applyBtn.Background(ui.Hex("#94A3B8"))
							} else {
								applyBtn.Background(ui.Hex("#007AFF"))
							}
							applyBtn.Children(func() {
								ui.Row(c).Center().Gap(4).Children(func() {
									if !(a.busy && isSelected) {
										ui.Icon(c, icons.Check).Size(12, 12).TextColor(ui.Hex("#FFFFFF"))
									}
									ui.Text(c, btnLabel).FontSize(11).Bold().TextColor(ui.Hex("#FFFFFF"))
								})
							})
							if applyBtn.Clicked() {
								a.selectedOnline = &item
								a.applyOnlineWallpaper(item)
							}
						})
					})
				}
			})
		})
	})
}

// drawLargePreviewBox 绘制大图预览区域 (内嵌 macOS Dock Simulator 拟真栏)
func (a *application) drawLargePreviewBox(c *ui.Context, t *ui.Theme) {
	ui.Box(c).Grow(1).FillWidth().MinHeight(320).Radius(14).Clip().Background(ui.Hex("#0B192C")).Border(1, ui.Hex("#00000010")).Children(func() {
		if a.preview == nil {
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Text(c, "还没有选择壁纸").FontSize(18).Bold().TextColor(ui.Hex("#94A3B8"))
				ui.Text(c, "支持 JPG、PNG、WebP 和 BMP").FontSize(12).TextColor(ui.Hex("#94A3B8"))
			})
			return
		}

		// 1. 壁纸实际图像填充
		ui.Image(c, a.preview).Fill().Fit(previewFit(a.fit)).Label("所选壁纸预览")

		// 2. 顶部浮动信息层 (Absolute 定位在卡片顶部)
		w, h := a.preview.Size()
		resolutionText := fmt.Sprintf("%d × %d", w, h)
		if spec := wallpaperSpec(a.selected, a.preview); spec != "" {
			resolutionText = fmt.Sprintf("%d × %d · %s", w, h, spec)
		}

		isApplied := a.applied != "" && a.selected == a.applied
		dotColor := ui.Hex("#007AFF")
		statusBadge := "预览模式 (未应用)"
		if isApplied {
			dotColor = ui.Hex("#10B981")
			statusBadge = "当前系统桌面壁纸"
		}

		ui.Row(c).Absolute().Top(16).Left(16).Right(16).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			// 左侧桌面状态胶囊
			ui.Box(c).Padding(5, 12).Radius(20).Background(ui.Hex("#00000070")).Border(1, ui.Hex("#FFFFFF20")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Box(c).Width(7).Height(7).Radius(4).Background(dotColor)
					ui.Text(c, statusBadge).FontSize(11).Bold().TextColor(ui.Hex("#FFFFFF"))
				})
			})

			// 右侧工具胶囊群：规格 + 收藏 + 另存为
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				// 1. 规格信息胶囊
				ui.Box(c).Padding(4, 10).Radius(20).Background(ui.Hex("#00000060")).Children(func() {
					ui.Text(c, resolutionText).FontSize(11).TextColor(ui.Hex("#E2E8F0"))
				})

				// 2. 收藏胶囊按钮
				isFav := a.IsFavorite(a.selected)
				favBg := ui.Hex("#00000070")
				favBorder := ui.Hex("#FFFFFF30")
				favTextCol := ui.Hex("#FFFFFF")
				favText := "收藏"
				if isFav {
					favBg = ui.Hex("#EF444440")
					favBorder = ui.Hex("#EF4444")
					favTextCol = ui.Hex("#FCA5A5")
					favText = "已收藏"
				}
				favBtn := ui.ButtonBase(c.Key("btn-preview-fav")).Padding(4, 10).Radius(20).Background(favBg).Border(1, favBorder).Disabled(a.selected == "")
				favBtn.Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
						if isFav {
							ui.Icon(c, icons.HeartFilled).Size(12, 12).TextColor(ui.Hex("#EF4444"))
						} else {
							ui.Icon(c, icons.Heart).Size(12, 12).TextColor(ui.Hex("#FFFFFF"))
						}
						ui.Text(c, favText).FontSize(11).Bold().TextColor(favTextCol)
					})
				})
				if favBtn.Clicked() {
					a.toggleFavorite(a.selected)
				}

				// 3. 另存为/导出胶囊按钮
				saveBtn := ui.ButtonBase(c.Key("btn-preview-export")).Padding(4, 10).Radius(20).Background(ui.Hex("#00000070")).Border(1, ui.Hex("#FFFFFF30")).Disabled(a.selected == "")
				saveBtn.Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
						ui.Icon(c, icons.Download).Size(12, 12).TextColor(ui.Hex("#FFFFFF"))
						ui.Text(c, "另存为").FontSize(11).TextColor(ui.Hex("#FFFFFF"))
					})
				})
				if saveBtn.Clicked() {
					a.exportWallpaper()
				}
			})
		})

		// 3. 底部居中悬浮：macOS Dock Simulator 拟真栏 (Absolute 定位在卡片底部)
		ui.Row(c).Absolute().Bottom(16).Left(0).Right(0).Justify(ui.Center).Children(func() {
			ui.Box(c).Radius(16).Background(ui.Hex("#FFFFFF30")).Border(1, ui.Hex("#FFFFFF40")).Padding(8, 14).Children(func() {
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					dockItems := []struct {
						label string
						icon  string
					}{
						{"Safari", "🧭"},
						{"终端", "⚡"},
						{"VS Code", "⟨/⟩"},
						{"访达", "📁"},
						{"音乐", "🎵"},
						{"设置", "⚙"},
					}
					for _, item := range dockItems {
						item := item
						ui.Box(c).Width(32).Height(32).Radius(8).Background(ui.Hex("#FFFFFFE0")).Border(1, ui.Hex("#00000010")).Center().Tooltip(item.label).Children(func() {
							ui.Text(c, item.icon).FontSize(15).TextColor(ui.Hex("#1E293B"))
						})
					}
				})
			})
		})
	})
}

// drawFitSelector 绘制壁纸填充模式选择器 (原型图胶囊风格)
func (a *application) drawFitSelector(c *ui.Context, t *ui.Theme) {
	ui.Box(c).Radius(8).Padding(3).Background(ui.Hex("#F1F5F9")).Children(func() {
		ui.Row(c).Gap(2).Children(func() {
			options := []struct {
				val   string
				label string
			}{
				{"覆盖", "⬒ 覆盖"},
				{"适应", "⬓ 适应"},
				{"拉伸", "⬖ 拉伸"},
			}
			for _, opt := range options {
				opt := opt
				isSelected := a.fit == opt.val
				btn := ui.Button(c.Key("seg-"+opt.val), opt.label).Padding(4, 10).Radius(6)
				if isSelected {
					btn.Background(ui.Hex("#FFFFFF")).TextColor(ui.Hex("#0F172A")).Bold()
				} else {
					btn.Background(ui.Hex("#00000000")).TextColor(ui.Hex("#64748B"))
				}
				if btn.Clicked() {
					a.fit = opt.val
				}
			}
		})
	})
}

// drawLocalBottomBar 绘制本地壁纸底部操作栏
func (a *application) drawLocalBottomBar(c *ui.Context, t *ui.Theme) {
	ui.Row(c).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
		// 左侧：状态圆点与当前状态
		dotColor := ui.Hex("#10B981")
		if a.error {
			dotColor = ui.Hex("#E81123")
		} else if a.busy {
			dotColor = ui.Hex("#007AFF")
		}
		statusMsg := "已成功应用到桌面并同步"
		if a.status != "" {
			statusMsg = a.status
		}

		ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Box(c).Width(8).Height(8).Radius(4).Background(dotColor)
			ui.Text(c, statusMsg).FontSize(12).TextColor(ui.Hex("#64748B")).SingleLine()
		})

		ui.Spacer(c)

		// 右侧操作按钮组 (在文件夹中显示、映射到 VS Code、VS Code 同步胶囊、应用到桌面)
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			// 1. 在文件夹中显示
			folderBtn := ui.ButtonBase(c.Key("btn-show-folder")).Height(36).Radius(8).Background(ui.Hex("#F8FAFC")).Border(1, ui.Hex("#E2E8F0")).Padding(0, 10).Disabled(a.selected == "")
			folderBtn.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
					ui.Icon(c, icons.ExternalLink).Size(12, 12).TextColor(ui.Hex("#334155"))
					ui.Text(c, "↗ 在文件夹中显示").FontSize(12).TextColor(ui.Hex("#334155"))
				})
			})
			if folderBtn.Clicked() {
				mygo.Shell.ShowItemInFolder(a.selected)
			}

			// 2. 映射到 VS Code (补全独立触发能力)
			vscodeBtn := ui.ButtonBase(c.Key("btn-map-vscode")).Height(36).Radius(8).Background(ui.Hex("#F8FAFC")).Border(1, ui.Hex("#E2E8F0")).Padding(0, 10).Disabled(a.selected == "" || a.busy)
			vscodeBtn.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
					ui.Icon(c, icons.Code).Size(12, 12).TextColor(ui.Hex("#007AFF"))
					ui.Text(c, "映射到 VS Code").FontSize(12).TextColor(ui.Hex("#007AFF"))
				})
			})
			if vscodeBtn.Clicked() {
				a.applyVSCode()
			}

			// 3. VS Code 同步状态胶囊
			syncLabel := "VS Code 自动同步中"
			syncCol := ui.Hex("#22C55E")
			if !a.syncVSCode {
				syncLabel = "同步到 VS Code"
				syncCol = ui.Hex("#94A3B8")
			}
			ui.Box(c).Height(36).Radius(8).Background(ui.Hex("#EFF6FF")).Border(1, ui.Hex("#BFDBFE")).Padding(0, 10).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Fill().Children(func() {
					ui.Icon(c, icons.Code).Size(12, 12).TextColor(ui.Hex("#007AFF"))
					syncBtn := ui.ButtonBase(c.Key("btn-sync-vscode")).Children(func() {
						ui.Text(c, syncLabel).FontSize(12).TextColor(ui.Hex("#007AFF"))
					})
					if syncBtn.Clicked() {
						a.syncVSCode = !a.syncVSCode
						a.toggleSyncVSCode()
					}
					ui.Box(c).Width(6).Height(6).Radius(3).Background(syncCol)
					ui.Text(c, "∨").FontSize(10).TextColor(ui.Hex("#007AFF"))
				})
			})

			// 4. 应用到桌面主按钮 (高亮主色)
			applyLabel := "✓ 应用到桌面"
			if a.busy {
				applyLabel = "正在应用…"
			}
			applyBtn := ui.ButtonBase(c.Key("btn-apply-desktop")).Height(36).Radius(8).Padding(0, 16).Disabled(a.selected == "" || a.busy)
			if a.selected == "" || a.busy {
				applyBtn.Background(ui.Hex("#94A3B8"))
			} else {
				applyBtn.Background(ui.Hex("#007AFF"))
			}
			applyBtn.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					if !a.busy {
						ui.Icon(c, icons.Check).Size(14, 14).TextColor(ui.Hex("#FFFFFF"))
					}
					ui.Text(c, applyLabel).FontSize(13).Bold().TextColor(ui.Hex("#FFFFFF"))
				})
			})
			if applyBtn.Clicked() {
				a.applyWallpaper()
			}
		})
	})
}

// drawOnlineBottomBar 绘制在线图库底部栏
func (a *application) drawOnlineBottomBar(c *ui.Context, t *ui.Theme) {
	// 在线大图预览态复用底部操作栏，支持直接设为壁纸与映射到 VS Code
	if a.onlineView == "preview" {
		a.drawLocalBottomBar(c, t)
		return
	}

	ui.Row(c).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
		// 左侧：状态圆点与当前状态
		statusMsg := fmt.Sprintf("已拉取必应今日精选 · 共 %d 张壁纸", len(a.wallpapers))
		if a.selectedCategory.Source == "360" {
			statusMsg = fmt.Sprintf("已拉取360壁纸 · 共 %d 张壁纸", len(a.wallpapers))
		}
		if a.status != "" && (a.error || a.busy) {
			statusMsg = a.status
		}

		dotColor := ui.Hex("#10B981")
		if a.error {
			dotColor = ui.Hex("#E81123")
		} else if a.busy || a.loadingOnline {
			dotColor = ui.Hex("#007AFF")
		}

		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Box(c).Width(7).Height(7).Radius(4).Background(dotColor)
			ui.Text(c, statusMsg).FontSize(12).TextColor(ui.Hex("#64748B")).SingleLine()
		})

		ui.Spacer(c)

		// 右侧：分页器群 (Pagination Cluster)
		totalPages := 3
		if a.selectedCategory.Source == "360" {
			totalPages = 10
		} else if a.selectedCategory.Source == "bing" {
			totalPages = 1
		}
		currentPage := a.onlinePage
		if currentPage < 1 {
			currentPage = 1
		}

		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			prevBtn := ui.ButtonBase(c.Key("page-prev")).Height(30).Padding(0, 10).Radius(6).Background(ui.Hex("#F8FAFC")).Border(1, ui.Hex("#E2E8F0")).Disabled(currentPage <= 1 || a.loadingOnline)
			prevBtn.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
					ui.Icon(c, icons.ChevronLeft).Size(11, 11).TextColor(ui.Hex("#94A3B8"))
					ui.Text(c, "◀ 上一页").FontSize(11).TextColor(ui.Hex("#94A3B8"))
				})
			})
			if prevBtn.Clicked() && currentPage > 1 {
				a.onlinePage--
				a.loadOnlineWallpapers(a.selectedCategory, a.onlinePage)
			}

			ui.Box(c).Padding(0, 8).Children(func() {
				ui.Text(c, fmt.Sprintf("第 %d / %d 页", currentPage, totalPages)).FontSize(11).Bold().TextColor(ui.Hex("#1E293B"))
			})

			nextBtn := ui.ButtonBase(c.Key("page-next")).Height(30).Padding(0, 10).Radius(6).Background(ui.Hex("#FFFFFF")).Border(1, ui.Hex("#E2E8F0")).Disabled(currentPage >= totalPages || a.loadingOnline)
			nextBtn.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
					ui.Text(c, "下一页 ▶").FontSize(11).TextColor(ui.Hex("#334155"))
					ui.Icon(c, icons.ChevronRight).Size(11, 11).TextColor(ui.Hex("#334155"))
				})
			})
			if nextBtn.Clicked() && currentPage < totalPages {
				a.onlinePage++
				a.loadOnlineWallpapers(a.selectedCategory, a.onlinePage)
			}
		})
	})
}

// drawBottomBar 兼容性方法
func (a *application) drawBottomBar(c *ui.Context, t *ui.Theme) {
	if a.tab == "online" {
		a.drawOnlineBottomBar(c, t)
	} else {
		a.drawLocalBottomBar(c, t)
	}
}

// 辅助方法：计算壁纸规格与缩略图
func (a *application) getWallpaperSpec(path string) string {
	a.thumbMu.Lock()
	bmp := a.recentBitmaps[path]
	a.thumbMu.Unlock()
	return wallpaperSpec(path, bmp)
}

func (a *application) getRecentThumbnail(path string) *ui.Bitmap {
	if a.selected == path && a.preview != nil {
		return a.preview
	}
	a.thumbMu.Lock()
	bmp, exists := a.recentBitmaps[path]
	a.thumbMu.Unlock()
	if exists {
		return bmp
	}
	return nil
}

func wallpaperSpec(path string, bmp *ui.Bitmap) string {
	var sizeStr string
	if fi, err := os.Stat(path); err == nil {
		bytes := fi.Size()
		if bytes >= 1024*1024 {
			sizeStr = fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
		} else if bytes >= 1024 {
			sizeStr = fmt.Sprintf("%.0f KB", float64(bytes)/1024)
		} else if bytes > 0 {
			sizeStr = fmt.Sprintf("%d B", bytes)
		}
	}
	if bmp != nil {
		w, h := bmp.Size()
		res := fmt.Sprintf("%d×%d", w, h)
		if w >= 5120 {
			res = "5K Retina"
		} else if w >= 3840 {
			res = "4K UHD"
		} else if w >= 2560 {
			res = "2K QHD"
		} else if w >= 1920 {
			res = "1080P FHD"
		}
		if sizeStr != "" {
			return res + " · " + sizeStr
		}
		return res
	}
	if sizeStr != "" {
		return "本地图片 · " + sizeStr
	}
	return "本地图片"
}

func categoryBadge(cat online.Category) string {
	if cat.Source == "bing" {
		return "8"
	}
	switch cat.ID {
	case "36":
		return "128"
	case "6":
		return "96"
	case "26":
		return "64"
	case "11":
		return "52"
	case "15":
		return "48"
	default:
		return "50+"
	}
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
			a.update(func() { a.setStatus("选择图片失败: "+err.Error(), true) })
			return
		}
		if len(paths) > 0 {
			a.update(func() { a.loadWallpaper(paths[0]) })
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
	a.thumbMu.Lock()
	a.recentBitmaps[path] = preview
	a.thumbMu.Unlock()
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
	a.update(func() {
		a.busy = true
		a.setStatus("正在映射到 VS Code 背景…", false)
	})
	go func() {
		report, err := a.vscode.Apply(path)
		if err != nil {
			a.update(func() {
				a.busy = false
				a.setStatus("映射到 VS Code 失败: "+err.Error(), true)
			})
			return
		}
		if !report.ExtensionInstalled {
			a.update(func() {
				a.setStatus("未检测到 BGGo 插件，正在自动下载并安装…", false)
			})
			installErr := a.vscode.DownloadAndInstallExtension(context.Background())
			a.update(func() {
				a.busy = false
				if installErr != nil {
					a.setStatus("已写入配置，但自动安装插件失败: "+installErr.Error()+"（可在 VS Code 手动安装）", true)
				} else {
					a.setStatus("已自动安装 VS Code 插件并映射背景（在 VS Code 中重新加载生效）", false)
				}
			})
			return
		}
		a.update(func() {
			a.busy = false
			a.setStatus("已映射到 VS Code 背景（在 VS Code 中重新加载生效）", false)
		})
	}()
}

func (a *application) applyWallpaper() {
	path := a.selected
	if path == "" {
		return
	}
	a.update(func() {
		a.busy = true
		a.setStatus("正在设置系统壁纸…", false)
	})
	syncVSCode := a.syncVSCode
	go func() {
		err := a.setter.Set(path)
		var vscodeErr error
		var vscodeReport *vscode.ApplyReport
		if err == nil && syncVSCode {
			vscodeReport, vscodeErr = a.vscode.Apply(path)
		}
		a.update(func() {
			a.busy = false
			if err != nil {
				a.setStatus(err.Error(), true)
				return
			}
			a.applied = path
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
	}()
}

// loadOnlineCategories 加载在线壁纸分类列表
func (a *application) loadOnlineCategories() {
	if a.onlineSvc == nil {
		return
	}
	a.update(func() {
		a.loadingOnline = true
		a.status = "正在加载壁纸分类…"
		a.error = false
	})
	go func() {
		cats, err := a.onlineSvc.GetCategories(context.Background())
		a.update(func() {
			a.loadingOnline = false
			if err != nil {
				a.status = "加载分类失败: " + err.Error()
				a.error = true
				return
			}
			a.categories = cats
			var firstCat *online.Category
			if len(cats) > 0 {
				a.selectedCategory = cats[0]
				a.onlinePage = 1
				cat := cats[0]
				firstCat = &cat
			}
			if firstCat != nil {
				go a.loadOnlineWallpapers(*firstCat, 1)
			}
		})
	}()
}

// loadOnlineWallpapers 拉取指定分类壁纸
func (a *application) loadOnlineWallpapers(cat online.Category, page int) {
	if a.onlineSvc == nil {
		return
	}
	a.update(func() {
		a.loadingOnline = true
		a.onlinePage = page
		a.selectedCategory = cat
		a.status = "正在拉取 " + cat.Name + " 壁纸…"
		a.error = false
	})
	go func() {
		pageSize := 18
		if cat.Source == "bing" {
			pageSize = 8
		}
		items, err := a.onlineSvc.GetWallpapers(context.Background(), cat, page, pageSize)
		a.update(func() {
			a.loadingOnline = false
			if err != nil {
				a.status = "拉取壁纸失败: " + err.Error()
				a.error = true
				return
			}
			a.wallpapers = items
			if len(items) > 0 {
				a.status = fmt.Sprintf("%s · 第 %d 页，共加载 %d 张", cat.Name, page, len(items))
			} else {
				a.status = "当前分类暂无壁纸"
			}
		})
	}()
}

// applyOnlineWallpaper 一键下载并应用在线壁纸
func (a *application) applyOnlineWallpaper(item online.WallpaperItem) {
	if a.onlineSvc == nil {
		return
	}
	var syncVSCode bool
	a.update(func() {
		a.busy = true
		a.status = "正在下载高清壁纸并应用…"
		a.error = false
		syncVSCode = a.syncVSCode
	})
	go func() {
		localPath, err := a.onlineSvc.DownloadWallpaper(context.Background(), item)
		if err != nil {
			a.update(func() {
				a.busy = false
				a.status = "下载壁纸失败: " + err.Error()
				a.error = true
			})
			return
		}

		var bmp *ui.Bitmap
		if data, readErr := os.ReadFile(localPath); readErr == nil {
			bmp, _ = ui.DecodeBitmap(data)
		}

		setErr := a.setter.Set(localPath)
		var vscodeErr error
		var vscodeReport *vscode.ApplyReport
		if setErr == nil && syncVSCode {
			vscodeReport, vscodeErr = a.vscode.Apply(localPath)
		}

		a.update(func() {
			a.busy = false
			a.selected = localPath
			if bmp != nil {
				a.preview = bmp
			}
			if a.store != nil {
				if state, saveErr := a.store.Add(localPath); saveErr == nil {
					a.recent = state.Recent
				}
			}

			if setErr != nil {
				a.status = "设置桌面壁纸失败: " + setErr.Error()
				a.error = true
				return
			}
			a.applied = localPath
			if syncVSCode {
				if vscodeErr != nil {
					a.status = "壁纸已应用，但同步到 VS Code 失败: " + vscodeErr.Error()
					a.error = true
				} else if vscodeReport != nil && !vscodeReport.ExtensionInstalled {
					go func() {
						_ = a.vscode.DownloadAndInstallExtension(context.Background())
					}()
					a.status = "壁纸已应用，正在后台自动安装 VS Code 插件…"
				} else {
					a.status = "壁纸已应用，并已同步到 VS Code"
				}
			} else {
				a.status = "壁纸已成功应用到桌面"
			}
		})
	}()
}

// previewOnlineWallpaper 加载高清原图并在大图视图中预览
func (a *application) previewOnlineWallpaper(item online.WallpaperItem) {
	if a.onlineSvc == nil {
		return
	}
	a.update(func() {
		a.busy = true
		a.status = "正在加载高清大图预览…"
		a.error = false
	})
	go func() {
		localPath, err := a.onlineSvc.DownloadWallpaper(context.Background(), item)
		if err != nil {
			a.update(func() {
				a.busy = false
				a.status = "加载大图失败: " + err.Error()
				a.error = true
			})
			return
		}
		var bmp *ui.Bitmap
		if data, readErr := os.ReadFile(localPath); readErr == nil {
			bmp, _ = ui.DecodeBitmap(data)
		}
		a.update(func() {
			a.busy = false
			a.selected = localPath
			if bmp != nil {
				a.preview = bmp
			}
			a.onlineView = "preview"
			a.status = "已加载高清预览: " + item.Title
		})
	}()
}

// requestThumbnail 异步请求缩略图
func (a *application) requestThumbnail(item online.WallpaperItem) {
	if a.onlineSvc == nil {
		return
	}
	a.thumbMu.Lock()
	if a.thumbBitmaps[item.ID] != nil || a.thumbLoading[item.ID] {
		a.thumbMu.Unlock()
		return
	}
	a.thumbLoading[item.ID] = true
	a.thumbMu.Unlock()

	go func(item online.WallpaperItem) {
		data, err := a.onlineSvc.FetchThumbnailBytes(context.Background(), item.ThumbURL)
		if err == nil {
			bmp, err := ui.DecodeBitmap(data)
			if err == nil {
				a.update(func() {
					a.thumbMu.Lock()
					a.thumbBitmaps[item.ID] = bmp
					delete(a.thumbLoading, item.ID)
					a.thumbMu.Unlock()
				})
				return
			}
		}
		a.update(func() {
			a.thumbMu.Lock()
			delete(a.thumbLoading, item.ID)
			a.thumbMu.Unlock()
		})
	}(item)
}

func (a *application) setStatus(message string, isError bool) {
	a.status, a.error = message, isError
}

func (a *application) checkForUpdates() {
	a.update(func() {
		a.setStatus("正在检查应用更新…", false)
	})
	if a.checkUpdate != nil {
		a.checkUpdate()
	}
}

func (a *application) initUpdater() {
	cfg := updater.Config{
		Repo:      "Delta1035/bggo",
		TagPrefix: "v",
		PublicKey: "Q6BKxOcOtIoRKdZIpkmUZzK6SePGA7EUGjIxgdjoIQ4=",
	}
	a.updaterSvc = updater.NewService(cfg)
	a.checkUpdate = a.handleCheckForUpdates
}

func (a *application) handleCheckForUpdates() {
	if a.updaterSvc == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		target := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
		res, err := a.updaterSvc.CheckUpdate(ctx, AppVersion, target)
		if err != nil {
			a.update(func() {
				a.setStatus("检查更新失败: "+err.Error(), true)
			})
			return
		}

		if !res.HasUpdate {
			a.update(func() {
				a.setStatus(fmt.Sprintf("BGGo 已是最新版本 (%s)", AppVersion), false)
			})
			if a.window != nil {
				_, _ = mygo.Dialog.Message(mygo.MessageOptions{
					Parent:  a.window,
					Type:    mygo.MessageInfo,
					Title:   "检查更新",
					Message: "当前已是最新版本",
					Detail:  fmt.Sprintf("BGGo %s 已是最新版本，无需更新。", AppVersion),
					Buttons: []string{"确定"},
				})
			}
			return
		}

		a.update(func() {
			a.setStatus(fmt.Sprintf("发现新版本 %s！", res.LatestVersion), false)
		})

		detail := fmt.Sprintf("BGGo 新版本 %s 已发布（当前版本 %s）。\n\n点击“立即更新”开始下载并安装。", res.LatestVersion, AppVersion)
		if res.Manifest != nil && res.Manifest.Notes != "" {
			detail = fmt.Sprintf("更新说明：\n%s\n\n点击“立即更新”开始下载并安装。", res.Manifest.Notes)
		}

		if a.window == nil {
			return
		}
		msgRes, _ := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:        a.window,
			Type:          mygo.MessageQuestion,
			Title:         "发现新版本",
			Message:       fmt.Sprintf("发现 BGGo 新版本 %s", res.LatestVersion),
			Detail:        detail,
			Buttons:       []string{"立即更新", "在浏览器中打开", "稍后"},
			DefaultButton: 0,
			CancelButton:  2,
		})

		switch msgRes.Button {
		case 0:
			go a.applyUpdate(res)
		case 1:
			releaseURL := fmt.Sprintf("https://github.com/%s/releases/tag/v%s", a.updaterSvc.Repo(), strings.TrimPrefix(res.LatestVersion, "v"))
			_ = a.updaterSvc.OpenBrowser(releaseURL)
		}
	}()
}

func (a *application) applyUpdate(res *updater.CheckResult) {
	if res == nil || res.Manifest == nil || a.updaterSvc == nil {
		return
	}

	a.update(func() {
		a.setStatus(fmt.Sprintf("正在下载 BGGo %s 更新包…", res.LatestVersion), false)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Linux 系统目录或 deb 包安装模式（如 /opt/bggo）
	if runtime.GOOS == "linux" && !a.updaterSvc.IsWritableInstall() {
		debURL := a.updaterSvc.GetDebDownloadURL(res.LatestVersion)
		cleanVer := strings.TrimPrefix(res.LatestVersion, "v")
		destFile := filepath.Join(os.TempDir(), fmt.Sprintf("bggo_%s_amd64.deb", cleanVer))

		err := a.updaterSvc.DownloadFile(ctx, debURL, destFile, func(downloaded, total int64) {
			if total > 0 {
				pct := int(downloaded * 100 / total)
				a.update(func() {
					a.setStatus(fmt.Sprintf("正在下载更新安装包… %d%%", pct), false)
				})
			}
		})
		if err != nil {
			a.update(func() {
				a.setStatus("下载更新包失败: "+err.Error(), true)
			})
			return
		}

		a.update(func() {
			a.setStatus("正在安装新版本…", false)
		})

		err = a.updaterSvc.InstallDebPackage(destFile)
		if err != nil {
			a.update(func() {
				a.setStatus("自动安装未完成，已保存安装包", true)
			})
			if a.window != nil {
				_, _ = mygo.Dialog.Message(mygo.MessageOptions{
					Parent:  a.window,
					Type:    mygo.MessageWarning,
					Title:   "更新安装提示",
					Message: "未能完成自动提权安装",
					Detail:  fmt.Sprintf("更新包已成功下载至:\n%s\n\n您可以打开终端手动安装:\nsudo dpkg -i %s", destFile, destFile),
					Buttons: []string{"确定"},
				})
			}
			return
		}

		a.update(func() {
			a.setStatus("更新安装完成！", false)
		})

		if a.window != nil {
			successRes, _ := mygo.Dialog.Message(mygo.MessageOptions{
				Parent:        a.window,
				Type:          mygo.MessageInfo,
				Title:         "更新完成",
				Message:       "BGGo 更新已安装完成",
				Detail:        "点击确定重启应用以体验新版本。",
				Buttons:       []string{"重启应用", "稍后重启"},
				DefaultButton: 0,
			})
			if successRes.Button == 0 {
				mygo.App.Relaunch()
			}
		}
		return
	}

	// 便携版或具有写权限目录安装模式（下载 tar.gz 校验并提示重启）
	archiveURL := res.Manifest.URL
	destArchive := filepath.Join(os.TempDir(), filepath.Base(archiveURL))
	err := a.updaterSvc.DownloadFile(ctx, archiveURL, destArchive, func(downloaded, total int64) {
		if total > 0 {
			pct := int(downloaded * 100 / total)
			a.update(func() {
				a.setStatus(fmt.Sprintf("正在下载更新包… %d%%", pct), false)
			})
		}
	})
	if err != nil {
		a.update(func() {
			a.setStatus("下载更新包失败: "+err.Error(), true)
		})
		return
	}

	data, err := os.ReadFile(destArchive)
	if err != nil {
		a.update(func() {
			a.setStatus("读取更新包失败: "+err.Error(), true)
		})
		return
	}
	if err := a.updaterSvc.VerifyArchive(data, res.Manifest.Signature); err != nil {
		a.update(func() {
			a.setStatus("更新签名校验未通过: "+err.Error(), true)
		})
		return
	}

	a.update(func() {
		a.setStatus("更新包校验通过，请重启应用生效", false)
	})
	if a.window != nil {
		successRes, _ := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:        a.window,
			Type:          mygo.MessageInfo,
			Title:         "更新准备就绪",
			Message:       fmt.Sprintf("BGGo %s 已准备就绪", res.LatestVersion),
			Detail:        "点击确定重启应用以完成更新。",
			Buttons:       []string{"重启应用", "稍后重启"},
			DefaultButton: 0,
		})
		if successRes.Button == 0 {
			mygo.App.Relaunch()
		}
	}
}

func (a *application) runSilentUpdateCheck() {
	if a.updaterSvc == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	target := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	res, err := a.updaterSvc.CheckUpdate(ctx, AppVersion, target)
	if err == nil && res != nil && res.HasUpdate {
		a.update(func() {
			a.setStatus(fmt.Sprintf("✨ 发现新版本 %s，点击“检查更新”即可安装", res.LatestVersion), false)
		})
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

func (a *application) exportWallpaper() {
	a.mu.Lock()
	path := a.selected
	win := a.window
	a.mu.Unlock()

	if path == "" || win == nil {
		return
	}
	ext := filepath.Ext(path)
	if ext == "" {
		ext = ".jpg"
	}
	go func() {
		dest, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      win,
			Title:       "导出壁纸另存为",
			DefaultPath: "wallpaper" + ext,
			Filters: []mygo.FileFilter{{
				Name:       "图片",
				Extensions: []string{"jpg", "jpeg", "png", "webp", "bmp"},
			}},
		})
		if err != nil || dest == "" {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			a.update(func() {
				a.status = "读取原始文件失败: " + err.Error()
				a.error = true
			})
			return
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			a.update(func() {
				a.status = "保存文件失败: " + err.Error()
				a.error = true
			})
			return
		}
		a.update(func() {
			a.status = "已成功导出壁纸到: " + filepath.Base(dest)
			a.error = false
		})
	}()
}

func (a *application) toggleFavorite(path string) {
	if path == "" || a.store == nil {
		return
	}
	state, isFav, err := a.store.ToggleFavorite(path)
	if err != nil {
		a.update(func() {
			a.status = "更新收藏失败: " + err.Error()
			a.error = true
		})
		return
	}
	a.update(func() {
		a.favorites = state.Favorites
		if isFav {
			a.status = "已加入收藏夹"
		} else {
			a.status = "已从收藏夹移除"
		}
		a.error = false
	})
}

func (a *application) clearRecent() {
	if a.store == nil {
		return
	}
	state, err := a.store.ClearRecent()
	if err != nil {
		a.update(func() {
			a.status = "清空最近壁纸失败: " + err.Error()
			a.error = true
		})
		return
	}
	a.update(func() {
		a.recent = state.Recent
		a.status = "已清空最近壁纸记录"
		a.error = false
	})
}

func (a *application) toggleAutoBing() {
	if a.store == nil {
		return
	}
	a.mu.Lock()
	next := a.autoBing
	a.mu.Unlock()
	state, err := a.store.SetAutoBing(next)
	if err == nil {
		a.update(func() {
			a.autoBing = state.AutoBing
			if a.autoBing {
				a.status = "已开启每日自动更新 Bing 壁纸"
			} else {
				a.status = "已关闭每日自动更新 Bing 壁纸"
			}
			a.error = false
		})
		if state.AutoBing {
			go a.runAutoBingCheck()
		}
	}
}

func (a *application) runAutoBingCheck() {
	if a.onlineSvc == nil {
		return
	}
	items, err := a.onlineSvc.Bing.Fetch(context.Background(), "bing_daily", 1, 1)
	if err != nil || len(items) == 0 {
		return
	}
	today := items[0]
	localPath, err := a.onlineSvc.DownloadWallpaper(context.Background(), today)
	if err != nil {
		return
	}
	if a.Selected() == localPath {
		return
	}
	var bmp *ui.Bitmap
	if data, err := os.ReadFile(localPath); err == nil {
		bmp, _ = ui.DecodeBitmap(data)
	}
	_ = a.setter.Set(localPath)
	var syncVSCode bool
	a.mu.Lock()
	syncVSCode = a.syncVSCode
	a.mu.Unlock()
	if syncVSCode {
		_, _ = a.vscode.Apply(localPath)
	}
	a.update(func() {
		a.selected = localPath
		if bmp != nil {
			a.preview = bmp
		}
		if a.store != nil {
			if state, saveErr := a.store.Add(localPath); saveErr == nil {
				a.recent = state.Recent
			}
		}
		a.status = "已自动同步今日必应壁纸: " + today.Title
		a.error = false
	})
}

func (a *application) applyRandomWallpaper() {
	if a.onlineSvc == nil {
		return
	}
	go func() {
		items, err := a.onlineSvc.Qihoo.Fetch(context.Background(), "9", 1, 30)
		if err != nil || len(items) == 0 {
			return
		}
		idx := time.Now().UnixNano() % int64(len(items))
		a.applyOnlineWallpaper(items[idx])
	}()
}

func (a *application) initTray() {
	if len(appIconBytes) == 0 {
		return
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{
			Label: "显示主窗口",
			Click: func(item *mygo.MenuItem, win *mygo.Window) {
				if a.window != nil {
					a.window.Show()
					a.window.Restore()
					a.window.Focus()
				}
			},
		},
		{
			Label: "设为今日必应壁纸",
			Click: func(item *mygo.MenuItem, win *mygo.Window) {
				go a.runAutoBingCheck()
			},
		},
		{
			Label: "随机换一张壁纸",
			Click: func(item *mygo.MenuItem, win *mygo.Window) {
				a.applyRandomWallpaper()
			},
		},
		mygo.Separator(),
		{
			Label: "退出 BGGo",
			Role:  mygo.RoleQuit,
			Click: func(item *mygo.MenuItem, win *mygo.Window) {
				a.quitting = true
				if a.tray != nil {
					a.tray.Destroy()
				}
				if a.window != nil {
					a.window.Close()
				}
				mygo.App.Quit()
			},
		},
	})

	tray, err := mygo.NewTray(mygo.TrayOptions{
		Icon:    appIconBytes,
		ToolTip: "BGGo 壁纸",
		Menu:    menu,
	})
	if err != nil {
		return
	}
	a.tray = tray
	tray.OnClick(func() {
		if a.window != nil {
			if a.window.IsVisible() && !a.window.IsMinimized() {
				a.window.Hide()
			} else {
				a.window.Show()
				a.window.Restore()
				a.window.Focus()
			}
		}
	})
}

func categoryIcon(name string) string {
	switch {
	case strings.Contains(name, "必应"):
		return "🌟"
	case strings.Contains(name, "4K"):
		return "🖥"
	case strings.Contains(name, "风景"):
		return "🏞"
	case strings.Contains(name, "动漫"):
		return "🌸"
	case strings.Contains(name, "美女"):
		return "💃"
	case strings.Contains(name, "爱情"):
		return "❤️"
	case strings.Contains(name, "游戏"):
		return "🎮"
	case strings.Contains(name, "萌宠") || strings.Contains(name, "动物"):
		return "🐱"
	case strings.Contains(name, "汽车"):
		return "🚗"
	case strings.Contains(name, "清新"):
		return "🌿"
	case strings.Contains(name, "视觉") || strings.Contains(name, "炫彩"):
		return "🎨"
	case strings.Contains(name, "明星"):
		return "✨"
	case strings.Contains(name, "影视"):
		return "🎬"
	case strings.Contains(name, "军事"):
		return "⚔️"
	case strings.Contains(name, "文字"):
		return "✍️"
	case strings.Contains(name, "体育"):
		return "⚽"
	default:
		return "🖼"
	}
}

func shouldPreventClose(quitting bool, hasTray bool) bool {
	return !quitting && hasTray
}

func main() {
	if mygo.Updater.Enabled() {
		mygo.Use(native.Plugin)
	}
	a := newApplication()
	mygo.App.WhenReady(func() {
		if runtime.GOOS == "darwin" {
			mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
				{Label: "BGGo", Submenu: []*mygo.MenuItem{
					{Role: mygo.RoleAbout},
					{
						Label: "检查更新…",
						Click: func(*mygo.MenuItem, *mygo.Window) { a.checkForUpdates() },
					},
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
			Frameless: true,
			StateKey:  "main",
			Content:   ui.View(a.view),
		})
		a.window.SetMenu(nil)

		a.window.OnFileDrop(func(e *mygo.FileDropEvent) {
			for _, p := range e.Paths {
				ext := strings.ToLower(filepath.Ext(p))
				if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".bmp" {
					a.update(func() {
						a.tab = "local"
						a.loadWallpaper(p)
					})
					break
				}
			}
		})

		mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
			a.quitting = true
		})

		a.window.OnClose(func(e *mygo.CloseEvent) {
			if shouldPreventClose(a.quitting, a.tray != nil) {
				e.PreventDefault()
				a.window.Hide()
			}
		})

		mygo.App.OnWillQuit(func(e *mygo.QuitEvent) {
			if a.tray != nil {
				a.tray.Destroy()
			}
		})
		a.initTray()

		time.AfterFunc(10*time.Second, func() {
			a.runSilentUpdateCheck()
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(fmt.Errorf("启动 BGGo: %w", err))
	}
}
