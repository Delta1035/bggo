package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"bggo/internal/library"
	"bggo/internal/online"
	"bggo/internal/vscode"
	"bggo/internal/wallpaper"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

//go:embed resources/icon.png
var appIconBytes []byte

const AppVersion = "v0.2.4"

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
	checkUpdate func()

	// 本地浏览子标签
	localSubTab string // "recent" | "favorites"

	// 在线壁纸相关
	onlineSvc        *online.Service
	tab              string // "local" | "online"
	onlineView       string // "grid" | "preview"
	categories       []online.Category
	selectedCategory online.Category
	wallpapers       []online.WallpaperItem
	selectedOnline   *online.WallpaperItem
	onlinePage       int
	loadingOnline    bool
	thumbBitmaps     map[string]*ui.Bitmap
	thumbLoading     map[string]bool
	thumbMu          sync.Mutex

	tray *mygo.Tray
	mu   sync.RWMutex
}

func newApplication() *application {
	a := &application{
		setter:       wallpaper.Service{},
		fit:          "覆盖",
		status:       "选择一张图片开始",
		checkUpdate:  updater.CheckForUpdates,
		tab:          "local",
		localSubTab:  "recent",
		onlineView:   "grid",
		onlinePage:   1,
		thumbBitmaps: make(map[string]*ui.Bitmap),
		thumbLoading: make(map[string]bool),
	}

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
	t := c.Theme()

	// 最外层窗口容器：在 Frameless 自绘模式下保持 1px 边框和微圆角
	ui.Column(c).Fill().Background(t.Background).Border(1, t.Border).Children(func() {
		// 1. 完全自绘的现代标题栏
		a.drawTitleBar(c, t)

		// 2. 窗口主体内容区 (侧边栏 + 主内容区)
		ui.Row(c).Grow(1).FillWidth().Children(func() {
			a.drawSidebar(c, t)
			a.drawMainArea(c, t)
		})
	})
}

// drawSidebar 绘制左侧侧边栏
func (a *application) drawSidebar(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Width(260).FillHeight().Padding(16, 16).Gap(14).Background(t.Surface).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
		// 1. 顶部模式切换胶囊 [ 💻 本地壁纸 | 🌐 在线图库 ]
		ui.Box(c).Radius(8).Padding(3).Background(t.Border).FillWidth().Children(func() {
			ui.Row(c).Gap(2).FillWidth().Children(func() {
				isLocal := a.tab == "local"
				btnLocal := ui.Button(c.Key("tab-local"), "💻 本地壁纸").Grow(1).Height(30).Radius(6)
				if isLocal {
					btnLocal.Background(t.Surface).TextColor(t.Text).Bold()
				} else {
					btnLocal.Background(t.Border).TextColor(t.TextMuted)
				}
				if btnLocal.Clicked() {
					a.tab = "local"
				}

				isOnline := a.tab == "online"
				btnOnline := ui.Button(c.Key("tab-online"), "🌐 在线图库").Grow(1).Height(30).Radius(6)
				if isOnline {
					btnOnline.Background(t.Surface).TextColor(t.Text).Bold()
				} else {
					btnOnline.Background(t.Border).TextColor(t.TextMuted)
				}
				if btnOnline.Clicked() {
					a.tab = "online"
					if len(a.categories) == 0 && !a.loadingOnline {
						a.loadOnlineCategories()
					}
				}
			})
		})

		// 2. 中间内容区：根据模式展示
		if a.tab == "local" {
			// 本地模式：选择本地图片主按钮
			if ui.PrimaryButton(c, "📁  选择本地图片").FillWidth().Height(36).Radius(8).Clicked() {
				a.chooseWallpaper()
			}
			ui.Row(c).Justify(ui.Center).AlignItems(ui.Center).FillWidth().Children(func() {
				ui.Text(c, "或直接拖放图片到窗口").FontSize(11).TextColor(t.TextMuted)
			})

			// 切换：[ 🕒 最近使用 | ❤️ 我的收藏 ]
			ui.Box(c).Radius(6).Padding(2).Background(t.Border).FillWidth().Children(func() {
				ui.Row(c).Gap(2).FillWidth().Children(func() {
					isRecent := a.localSubTab == "recent"
					btnRecent := ui.Button(c.Key("sub-recent"), fmt.Sprintf("🕒 最近 (%d)", len(a.recent))).Grow(1).Height(26).Radius(4)
					if isRecent {
						btnRecent.Background(t.Surface).TextColor(t.Text).Bold()
					} else {
						btnRecent.Background(t.Border).TextColor(t.TextMuted)
					}
					if btnRecent.Clicked() {
						a.localSubTab = "recent"
					}

					isFav := a.localSubTab == "favorites"
					btnFav := ui.Button(c.Key("sub-fav"), fmt.Sprintf("❤️ 收藏 (%d)", len(a.favorites))).Grow(1).Height(26).Radius(4)
					if isFav {
						btnFav.Background(t.Surface).TextColor(t.Text).Bold()
					} else {
						btnFav.Background(t.Border).TextColor(t.TextMuted)
					}
					if btnFav.Clicked() {
						a.localSubTab = "favorites"
					}
				})
			})

			// 列表渲染 (最近使用 或 收藏夹)
			ui.Column(c).Grow(1).Gap(6).FillWidth().Children(func() {
				currentList := a.recent
				emptyText := "还没有使用过壁纸"
				if a.localSubTab == "favorites" {
					currentList = a.favorites
					emptyText = "还没有收藏任何壁纸"
				} else if len(a.recent) > 0 {
					ui.Row(c).AlignItems(ui.Center).FillWidth().Children(func() {
						ui.Text(c, "历史记录").FontSize(11).Bold().TextColor(t.TextMuted)
						ui.Spacer(c)
						if ui.Button(c.Key("clear-recent"), "清空").Padding(1, 6).Radius(4).Clicked() {
							a.clearRecent()
						}
					})
				}
				ui.Scroll(c).Grow(1).Gap(4).Children(func() {
					if len(currentList) == 0 {
						ui.Text(c, emptyText).FontSize(12).TextColor(t.TextMuted).Margin(6, 0)
					}
					for _, path := range currentList {
						path := path
						isSelected := a.selected == path
						icon := "🖼 "
						if a.localSubTab == "favorites" {
							icon = "❤️ "
						}
						btn := ui.Button(c.Key("item-"+path), icon+" "+filepath.Base(path)).FillWidth().Radius(6).Padding(6, 8).Tooltip(path)
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
		} else {
			// 在线模式：壁纸分类列表
			ui.Column(c).Grow(1).Gap(6).FillWidth().Children(func() {
				ui.Row(c).AlignItems(ui.Center).FillWidth().Children(func() {
					ui.Text(c, "壁纸分类").FontSize(11).Bold().TextColor(t.TextMuted)
					ui.Spacer(c)
					if ui.Button(c.Key("refresh-cats"), "↻ 刷新").Padding(2, 6).Radius(4).Clicked() {
						a.loadOnlineCategories()
					}
				})

				ui.Scroll(c).Grow(1).Gap(4).Children(func() {
					if len(a.categories) == 0 {
						if a.loadingOnline {
							ui.Text(c, "正在加载分类…").FontSize(12).TextColor(t.TextMuted).Margin(6, 0)
						} else {
							ui.Text(c, "暂无分类数据").FontSize(12).TextColor(t.TextMuted).Margin(6, 0)
						}
					}
					for _, cat := range a.categories {
						cat := cat
						isSelected := a.selectedCategory.ID == cat.ID && a.selectedCategory.Source == cat.Source
						icon := categoryIcon(cat.Name)
						btn := ui.Button(c.Key("cat-"+cat.Source+"-"+cat.ID), icon+"  "+cat.Name).FillWidth().Radius(6).Padding(6, 8)
						if isSelected {
							btn.Background(t.Accent).TextColor(t.AccentText).Bold()
						} else {
							btn.Background(t.Surface).TextColor(t.Text)
						}
						if btn.Clicked() {
							a.onlinePage = 1
							a.selectedCategory = cat
							a.onlineView = "grid"
							a.loadOnlineWallpapers(cat, 1)
						}
					}
				})
			})
		}

		// 3. 底部信息与检查更新
		ui.Column(c).Gap(10).FillWidth().Children(func() {
			// 每日必应自动更新
			ui.Row(c).AlignItems(ui.Center).Gap(6).FillWidth().Children(func() {
				if ui.Switch(c, &a.autoBing).Changed() {
					a.toggleAutoBing()
				}
				ui.Text(c, "每日自动必应壁纸").FontSize(11).TextColor(t.TextMuted)
			})

			if ui.Button(c, "↻  检查更新").FillWidth().Height(32).Radius(6).Clicked() {
				a.checkForUpdates()
			}
			ui.Row(c).Justify(ui.Center).FillWidth().Children(func() {
				ui.Text(c, "BGGo 桌面版 · "+AppVersion).FontSize(11).TextColor(t.TextMuted)
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
	ui.Column(c).Grow(1).FillHeight().Padding(18, 24, 20, 24).Gap(16).Children(func() {
		// 1. 顶部栏：标题、路径与 Segmented Control 胶囊分段选择器
		ui.Row(c).AlignItems(ui.Center).FillWidth().Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, "桌面壁纸").FontSize(22).Bold().TextColor(t.Text)
				ui.Text(c, selectedLabel(a.selected)).FontSize(12).TextColor(t.TextMuted).MaxWidth(480).SingleLine()
			})
			ui.Spacer(c)

			// 原生胶囊分段选择器 [ 覆盖 | 适应 | 拉伸 ]
			a.drawFitSelector(c, t)
		})

		// 2. 中央大图预览卡片
		a.drawLargePreviewBox(c, t)

		// 3. 底部操作栏
		a.drawBottomBar(c, t)
	})
}

// drawOnlineMain 绘制在线壁纸主页面
func (a *application) drawOnlineMain(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).FillHeight().Padding(18, 24, 20, 24).Gap(16).Children(func() {
		// 1. 顶部控制栏：分类名、子视图切换 [ 网格 | 高清预览 ]、分页控件
		ui.Row(c).AlignItems(ui.Center).FillWidth().Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				catName := a.selectedCategory.Name
				if catName == "" {
					catName = "在线壁纸"
				}
				sourceText := "数据源: 微软必应官方"
				if a.selectedCategory.Source == "360" {
					sourceText = "数据源: 360壁纸库 (国内CDN)"
				}
				ui.Text(c, "在线壁纸 · "+catName).FontSize(20).Bold().TextColor(t.Text)
				ui.Text(c, sourceText).FontSize(12).TextColor(t.TextMuted)
			})
			ui.Spacer(c)

			// 子视图切换 [ ⊞ 壁纸列表 | 🖼 高清预览 ]
			ui.Box(c).Radius(8).Padding(3).Background(t.Border).Children(func() {
				ui.Row(c).Gap(2).Children(func() {
					isGrid := a.onlineView == "grid"
					btnGrid := ui.Button(c.Key("subview-grid"), "⊞ 壁纸列表").Padding(4, 10).Radius(6)
					if isGrid {
						btnGrid.Background(t.Surface).TextColor(t.Text).Bold()
					} else {
						btnGrid.Background(t.Border).TextColor(t.TextMuted)
					}
					if btnGrid.Clicked() {
						a.onlineView = "grid"
					}

					isPreview := a.onlineView == "preview"
					btnPrev := ui.Button(c.Key("subview-prev"), "🖼 高清预览").Padding(4, 10).Radius(6)
					if isPreview {
						btnPrev.Background(t.Surface).TextColor(t.Text).Bold()
					} else {
						btnPrev.Background(t.Border).TextColor(t.TextMuted)
					}
					if btnPrev.Clicked() {
						a.onlineView = "preview"
					}
				})
			})

			// 适应模式选择器 (在大图预览时显示)
			if a.onlineView == "preview" {
				ui.Row(c).Margin(0, 0, 0, 8).Children(func() {
					a.drawFitSelector(c, t)
				})
			}
		})

		// 2. 主体展示区 (网格卡片 或 大图预览)
		if a.onlineView == "preview" {
			a.drawLargePreviewBox(c, t)
		} else {
			a.drawWallpaperGrid(c, t)
		}

		// 3. 底部操作栏
		a.drawBottomBar(c, t)
	})
}

// drawWallpaperGrid 绘制网格卡片列表
func (a *application) drawWallpaperGrid(c *ui.Context, t *ui.Theme) {
	ui.Box(c).Grow(1).FillWidth().MinHeight(260).Radius(14).Clip().Background(t.Surface).Border(1, t.Border).Padding(12, 12).Children(func() {
		if a.loadingOnline && len(a.wallpapers) == 0 {
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Text(c, "正在加载在线壁纸…").FontSize(16).Bold().TextColor(t.TextMuted)
				ui.Text(c, "正在连接国内图库源，请稍候").FontSize(12).TextColor(t.TextMuted)
			})
			return
		}

		if len(a.wallpapers) == 0 {
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
				for _, item := range a.wallpapers {
					item := item
					isSelected := a.selectedOnline != nil && a.selectedOnline.ID == item.ID
					borderColor := t.Border
					if isSelected {
						borderColor = t.Accent
					}

					ui.Column(c.Key("card-"+item.ID)).Padding(8, 8).Gap(8).Radius(10).Background(t.Background).Border(1, borderColor).Children(func() {
						// 缩略图
						a.thumbMu.Lock()
						bmp := a.thumbBitmaps[item.ID]
						a.thumbMu.Unlock()

						if bmp != nil {
							ui.Image(c, bmp).Height(120).FillWidth().Radius(6).Fit(ui.Cover)
						} else {
							a.requestThumbnail(item)
							ui.Box(c).Height(120).FillWidth().Radius(6).Background(t.Border).Center().Children(func() {
								ui.Text(c, "加载中…").FontSize(11).TextColor(t.TextMuted)
							})
						}

						// 标题与分辨率
						ui.Row(c).AlignItems(ui.Center).FillWidth().Children(func() {
							ui.Text(c, item.Title).FontSize(12).Bold().TextColor(t.Text).Grow(1).SingleLine()
							if item.Resolution != "" {
								ui.Box(c).Padding(2, 5).Radius(4).Background(t.Border).Children(func() {
									ui.Text(c, item.Resolution).FontSize(10).TextColor(t.TextMuted)
								})
							}
						})

						// 卡片操作按钮行：预览大图 / 设为壁纸
						ui.Row(c).Gap(6).FillWidth().Children(func() {
							if ui.Button(c.Key("btn-prev-"+item.ID), "👁 预览").Grow(1).Height(28).Radius(6).Clicked() {
								a.selectedOnline = &item
								a.previewOnlineWallpaper(item)
							}
							btnLabel := "应用"
							if a.busy && isSelected {
								btnLabel = "正在应用…"
							}
							if ui.PrimaryButton(c.Key("btn-apply-"+item.ID), btnLabel).Grow(1).Height(28).Radius(6).Disabled(a.busy).Clicked() {
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

// drawLargePreviewBox 绘制大图预览区域
func (a *application) drawLargePreviewBox(c *ui.Context, t *ui.Theme) {
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
			// 左上角悬浮胶囊：区分“当前桌面生效中”与“预览中 (未应用)”
			isApplied := a.applied != "" && a.selected == a.applied
			dotColor := t.Accent
			statusBadge := "预览模式 (未应用)"
			if isApplied {
				dotColor = t.Success
				statusBadge = "当前桌面生效中"
			}
			ui.Box(c).Padding(3, 9).Radius(12).Background(t.Surface).Border(1, t.Border).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
					ui.Box(c).Width(6).Height(6).Radius(3).Background(dotColor)
					ui.Text(c, statusBadge).FontSize(11).Bold().TextColor(t.Text)
				})
			})
			ui.Spacer(c)
			// 右上角悬浮胶囊：分辨率尺寸
			ui.Box(c).Padding(3, 9).Radius(12).Background(t.Surface).Border(1, t.Border).Children(func() {
				ui.Text(c, resolutionText).FontSize(11).TextColor(t.TextMuted)
			})
		})
	})
}

// drawFitSelector 绘制壁纸填充模式选择器
func (a *application) drawFitSelector(c *ui.Context, t *ui.Theme) {
	ui.Box(c).Radius(8).Padding(3).Background(t.Border).Children(func() {
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
				btn := ui.Button(c.Key("seg-"+opt.val), opt.label).Padding(4, 12).Radius(6)
				if isSelected {
					btn.Background(t.Surface).TextColor(t.Text).Bold()
				} else {
					btn.Background(t.Border).TextColor(t.TextMuted)
				}
				if btn.Clicked() {
					a.fit = opt.val
				}
			}
		})
	})
}

// drawBottomBar 绘制底部操作与状态栏
func (a *application) drawBottomBar(c *ui.Context, t *ui.Theme) {
	ui.Row(c).AlignItems(ui.Center).Gap(12).FillWidth().Children(func() {
		// 状态指示灯
		dotColor := t.Success
		if a.error {
			dotColor = t.Danger
		} else if a.busy || a.loadingOnline {
			dotColor = t.Accent
		}
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Box(c).Width(8).Height(8).Radius(4).Background(dotColor)
			ui.Text(c, a.status).FontSize(12).TextColor(t.TextMuted).SingleLine()
		})

		ui.Spacer(c)

		// 在线网格分页控件（放置于底部栏，符合用户自上而下的浏览动线）
		if a.tab == "online" && a.onlineView == "grid" && a.selectedCategory.Source == "360" {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				prevBtn := ui.Button(c.Key("page-prev"), "◀ 上一页").Padding(4, 8).Radius(6).Disabled(a.onlinePage <= 1 || a.loadingOnline)
				if prevBtn.Clicked() && a.onlinePage > 1 {
					a.onlinePage--
					a.loadOnlineWallpapers(a.selectedCategory, a.onlinePage)
				}
				ui.Text(c, fmt.Sprintf("第 %d 页", a.onlinePage)).FontSize(12).TextColor(t.TextMuted)
				nextBtn := ui.Button(c.Key("page-next"), "下一页 ▶").Padding(4, 8).Radius(6).Disabled(a.loadingOnline)
				if nextBtn.Clicked() {
					a.onlinePage++
					a.loadOnlineWallpapers(a.selectedCategory, a.onlinePage)
				}
			})
		}

		// 同步到 VS Code 开关
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			if ui.Switch(c, &a.syncVSCode).Changed() {
				a.toggleSyncVSCode()
			}
			ui.Text(c, "同步到 VS Code").FontSize(12).TextColor(t.Text)
		})

		// 收藏与另存为
		if a.selected != "" {
			favLabel := "🤍 收藏"
			if a.IsFavorite(a.selected) {
				favLabel = "❤️ 已收藏"
			}
			if ui.Button(c.Key("btn-toggle-fav"), favLabel).Height(36).Radius(8).Clicked() {
				a.toggleFavorite(a.selected)
			}
			if ui.Button(c.Key("btn-export"), "💾 另存为").Height(36).Radius(8).Clicked() {
				a.exportWallpaper()
			}
		}

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
}

// drawTitleBar 绘制完全自绘的现代化标题栏
func (a *application) drawTitleBar(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Height(38).FillWidth().Background(t.Surface).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).AlignItems(ui.Center).DragWindow().Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(0, 14).Children(func() {
			ui.Box(c).Width(22).Height(22).Radius(6).Background(t.Accent).Center().Children(func() {
				ui.Text(c, "🖼").FontSize(12).TextColor(t.AccentText)
			})
			ui.Text(c, "BGGo 壁纸").FontSize(13).Bold().TextColor(t.Text)
			ui.Box(c).Padding(2, 7).Radius(10).Background(t.Border).Children(func() {
				ui.Text(c, AppVersion).FontSize(10).Bold().TextColor(t.TextMuted)
			})
		})

		ui.Spacer(c)

		ui.Row(c).Height(38).AlignItems(ui.Center).Children(func() {
			minBtn := ui.ButtonBase(c.Key("win-min")).Width(44).Height(38)
			if minBtn.Hovered() {
				minBtn.Background(t.Border)
			}
			minBtn.Children(func() {
				ui.Text(c, "—").FontSize(11).TextColor(t.Text)
			})
			if minBtn.Clicked() && a.window != nil {
				a.window.Minimize()
			}

			isMax := a.window != nil && a.window.IsMaximized()
			maxIcon := "□"
			if isMax {
				maxIcon = "❐"
			}
			maxBtn := ui.ButtonBase(c.Key("win-max")).Width(44).Height(38)
			if maxBtn.Hovered() {
				maxBtn.Background(t.Border)
			}
			maxBtn.Children(func() {
				ui.Text(c, maxIcon).FontSize(12).TextColor(t.Text)
			})
			if maxBtn.Clicked() && a.window != nil {
				a.window.ToggleMaximize()
			}

			closeBtn := ui.ButtonBase(c.Key("win-close")).Width(48).Height(38)
			closeTextColor := t.Text
			if closeBtn.Hovered() {
				closeBtn.Background(ui.Hex("#E81123"))
				closeTextColor = ui.Hex("#FFFFFF")
			}
			closeBtn.Children(func() {
				ui.Text(c, "✕").FontSize(12).TextColor(closeTextColor)
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
			Click: func(item *mygo.MenuItem, win *mygo.Window) {
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

		a.window.OnClose(func(e *mygo.CloseEvent) {
			if a.tray != nil {
				e.PreventDefault()
				a.window.Hide()
			}
		})
		a.initTray()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(fmt.Errorf("启动 BGGo: %w", err))
	}
}
