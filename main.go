package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bggo/internal/icons"
	"bggo/internal/library"
	"bggo/internal/online"
	"bggo/internal/plugin"
	"bggo/internal/updater"
	"bggo/internal/vscode"
	"bggo/internal/wallpaper"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"github.com/harukizmoe/mayu"
	"github.com/harukizmoe/mayu/components"
	"github.com/harukizmoe/mayu/symbols"
	"github.com/harukizmoe/mayu/theme"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

//go:embed resources/icon.png
var appIconBytes []byte

const AppVersion = "v0.2.12"

// m3SeedColor is the brand blue that seeds Mayu's Material You tonal palettes.
// Both light and dark schemes are derived from it at runtime.
var m3SeedColor = ui.Hex("#007AFF")

// m3Scheme returns the active Material 3 color scheme for the current frame.
func m3Scheme(c *ui.Context) theme.ColorScheme { return mayu.Scheme(c) }

const (
	maxWallpaperFileBytes int64 = 64 << 20
	maxWallpaperPixels    int64 = 40_000_000
)

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
	onlineSvc                 *online.Service
	plugins                   []*plugin.Plugin
	pluginRecords             map[string]*managedPlugin
	pluginRoot                string
	pluginDataRoot            string
	pluginStatePath           string
	pluginState               plugin.EnabledState
	pluginMetaPath            string
	pluginMetadata            plugin.MetadataState
	pluginCatalogClient       *plugin.CatalogClient
	pluginUpdates             map[string]plugin.CatalogEntry
	pendingPluginZip          string
	pendingManifest           *plugin.Manifest
	pendingPermissionExpanded bool
	pluginStatus              string
	tab                       string // "local" | "online" | "plugins"
	onlineView                string // "grid" | "preview"
	searchKeyword             string // 在线壁纸搜索关键词
	filterOption              string // 筛选选项: 精选推荐/最新壁纸/最高人气
	categories                []online.Category
	selectedCategory          online.Category
	wallpapers                []online.WallpaperItem
	selectedOnline            *online.WallpaperItem
	onlinePage                int
	onlineMore                bool
	onlineTotal               int
	loadingOnline             bool
	thumbBitmaps              map[string]*ui.Bitmap
	thumbLoading              map[string]bool
	recentBitmaps             map[string]*ui.Bitmap
	thumbMu                   sync.Mutex
	loadSeq                   atomic.Uint64

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
	a.initPluginManagement()

	if onlineSvc, err := online.NewService(""); err == nil {
		a.onlineSvc = onlineSvc
		if a.pluginDataRoot != "" {
			a.plugins = loadBuiltinWallpaperPluginsAt(onlineSvc, a.pluginDataRoot)
		}
		a.registerBuiltinPluginRecords()
		a.loadInstalledPluginRecords()
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
	if state.Notice != "" {
		a.setStatus(state.Notice, true)
	}
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
	// Material You: follow the system appearance. c.Theme().Dark already
	// reflects the desktop preference; MyGo repaints the view on change.
	mayu.Use(c, mayu.ThemeOptions{
		SeedColor: m3SeedColor,
		Dark:      c.Theme().Dark,
		Density:   mayu.DensityCompact,
	})
	t := c.Theme()
	scheme := m3Scheme(c)

	// Outermost window container: 1px hairline border in the M3 outline color.
	ui.Column(c).Fill().Background(scheme.Background).Border(1, scheme.OutlineVariant).Children(func() {
		// 1. Self-drawn title bar with brand, version pill and window controls.
		a.drawTitleBar(c, t)

		// 2. Body: 80dp M3 navigation rail + contextual panel + active page.
		ui.Row(c).Grow(1).FillWidth().Children(func() {
			a.drawNavigationRail(c, t)
			a.drawContextPanel(c, t)
			a.drawMainArea(c, t)
		})
	})
}

// drawNavigationRail replaces the old 260px sidebar with an M3 NavigationRail.
// The recent/favorites list it used to host now lives in the local page's
// library panel, so all existing labels and click targets remain reachable.
func (a *application) drawNavigationRail(c *ui.Context, t *ui.Theme) {
	scheme := m3Scheme(c)
	// Self-drawn M3 rail: the icon and label live inside the SAME click target.
	// Mayu v0.1.1's NavigationRail builds the icon indicator as a sibling of the
	// clickable column, so tapping the icon never fires OnSelect. This keeps the
	// 80dp M3 look while making the whole destination item clickable.
	ui.Column(c).Width(80).FillHeight().Padding(12, 0).Justify(ui.SpaceBetween).AlignItems(ui.Center).
		Background(scheme.Surface).
		BorderWidth(0, 1, 0, 0).BorderColor(scheme.OutlineVariant).Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Box(c).Size(44, 44).Radius(14).Background(scheme.Primary).Center().Children(func() {
				ui.Icon(c, icons.Image).Size(24, 24).TextColor(scheme.OnPrimary)
			})

			destinations := []struct {
				key   string
				label string
				icon  *ui.SVG
			}{
				{"local", "本地壁纸", icons.Monitor},
				{"online", "在线图库", icons.Globe},
				{"plugins", "插件管理", icons.Download},
			}
			for _, dest := range destinations {
				dest := dest
				selected := a.tab == dest.key
				item := ui.ButtonBase(c.Key("rail-" + dest.key)).Width(72).Height(64).Radius(16).Label(dest.label)
				if selected {
					item.Background(scheme.SecondaryContainer)
				}
				item.Children(func() {
					ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
						iconColor := scheme.OnSurfaceVariant
						if selected {
							iconColor = scheme.OnSecondaryContainer
						}
						ui.Box(c).Size(56, 32).Radius(16).Center().Children(func() {
							ui.Icon(c, dest.icon).Size(22, 22).TextColor(iconColor)
						})
						labelColor := scheme.OnSurfaceVariant
						if selected {
							labelColor = scheme.OnSurface
						}
						ui.Text(c, dest.label).FontSize(11).Bold().TextColor(labelColor).SingleLine()
					})
				})
				if item.Clicked() {
					a.tab = dest.key
					if a.tab == "online" && len(a.categories) == 0 && !a.loadingOnline {
						a.loadOnlineCategories()
					}
				}
			}
		})

		chk := ui.ButtonBase(c.Key("rail-check-update")).Size(44, 44).Radius(22).Center().Tooltip("检查更新").Label("检查更新")
		chk.Children(func() {
			ui.Icon(c, icons.Sparkles).Size(18, 18).TextColor(scheme.Primary)
		})
		if chk.Clicked() {
			a.checkForUpdates()
		}
	})
}

// drawTitleBar renders the self-drawn full-width title bar using M3 tokens.
func (a *application) drawTitleBar(c *ui.Context, t *ui.Theme) {
	scheme := m3Scheme(c)
	// 40dp title bar: drag region; double-click toggles maximize.
	ui.Row(c).Height(40).FillWidth().Background(scheme.SurfaceContainerLow).BorderWidth(0, 0, 1, 0).BorderColor(scheme.OutlineVariant).AlignItems(ui.Center).DragWindow().Children(func() {
		// Brand: icon + title + version pill.
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(0, 14).Children(func() {
			ui.Box(c).Width(24).Height(24).Radius(8).Background(scheme.Primary).Center().Children(func() {
				ui.Icon(c, icons.Image).Size(14, 14).TextColor(scheme.OnPrimary)
			})
			ui.Text(c, "BGGo 壁纸").FontSize(13).Bold().TextColor(scheme.OnSurface)
			ui.Box(c).Padding(2, 7).Radius(10).Background(scheme.SurfaceContainerHighest).Center().Children(func() {
				ui.Text(c, AppVersion).FontSize(10).Bold().TextColor(scheme.OnSurfaceVariant)
			})
		})

		ui.Spacer(c)

		// Window controls: Lucide icons with accessible labels (icon-optimization
		// commit) sized for the 40dp M3 title bar.
		ui.Row(c).Height(40).AlignItems(ui.Center).Children(func() {
			// 1. Minimize
			minBtn := ui.ButtonBase(c.Key("win-min")).Width(44).Height(40).Label("最小化").Tooltip("最小化")
			if minBtn.Hovered() {
				minBtn.Background(scheme.SurfaceContainerHighest)
			}
			minBtn.Center().Children(func() {
				ui.Icon(c, icons.Minus).Size(12, 12).TextColor(scheme.OnSurfaceVariant)
			})
			if minBtn.Clicked() && a.window != nil {
				a.window.Minimize()
			}

			isMax := a.window != nil && a.window.IsMaximized()
			maxIcon := icons.Square
			maxTooltip := "最大化"
			if isMax {
				maxIcon = icons.Copy
				maxTooltip = "还原"
			}
			maxBtn := ui.ButtonBase(c.Key("win-max")).Width(44).Height(40).Label(maxTooltip).Tooltip(maxTooltip)
			if maxBtn.Hovered() {
				maxBtn.Background(scheme.SurfaceContainerHighest)
			}
			maxBtn.Center().Children(func() {
				ui.Icon(c, maxIcon).Size(12, 12).TextColor(scheme.OnSurfaceVariant)
			})
			if maxBtn.Clicked() && a.window != nil {
				a.window.ToggleMaximize()
			}

			// 3. Close button (red background on hover)
			closeBtn := ui.ButtonBase(c.Key("win-close")).Width(44).Height(40).Label("关闭").Tooltip("关闭")
			closeColor := scheme.OnSurfaceVariant
			if closeBtn.Hovered() {
				closeBtn.Background(scheme.Error)
				closeColor = scheme.OnError
			}
			closeBtn.Center().Children(func() {
				ui.Icon(c, icons.X).Size(13, 13).TextColor(closeColor)
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

// drawContextPanel renders the M3 companion panel next to the navigation
// rail. It hosts the local library list (recent/favorites) or the online
// category list, keeping every label the previous sidebar exposed.
func (a *application) drawContextPanel(c *ui.Context, t *ui.Theme) {
	scheme := m3Scheme(c)
	ui.Column(c).Width(248).FillHeight().Padding(14, 12).Gap(12).
		Background(scheme.SurfaceContainerLow).
		BorderWidth(0, 1, 0, 0).BorderColor(scheme.OutlineVariant).Children(func() {
		switch a.tab {
		case "local":
			a.drawLocalContext(c, scheme)
		case "online":
			a.drawOnlineContext(c, scheme)
		default:
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				ui.Text(c, "插件管理").FontSize(13).Bold().TextColor(scheme.OnSurface)
				ui.Text(c, "安装本地 ZIP 插件包，管理壁纸源生命周期").FontSize(11).TextColor(scheme.OnSurfaceVariant)
				ui.Box(c).Padding(12).Radius(12).Background(scheme.SecondaryContainer).Children(func() {
					ui.Text(c, fmt.Sprintf("已登记 %d 个插件", len(a.pluginRecords))).FontSize(12).Bold().TextColor(scheme.OnSecondaryContainer)
				})
			})
		}
	})
}

// drawLocalContext hosts the local library: import action, drop hint, the
// recent/favorites sub-tabs and the scrollable item list.
func (a *application) drawLocalContext(c *ui.Context, scheme theme.ColorScheme) {
	choose := mayu.FilledButton(c, "选择本地壁纸").Icon(symbols.Add).OnClick(a.chooseWallpaper)
	choose.Width(220).Height(40)
	ui.Row(c).AlignItems(ui.Center).FillWidth().Children(func() {
		ui.Text(c, "或直接拖放图片到窗口").FontSize(11).TextColor(scheme.OnSurfaceVariant)
	})

	// Sub-tabs: 最近使用 / 我的收藏. Both labels stay visible.
	ui.Box(c).Radius(16).Padding(2).FillWidth().Background(scheme.SurfaceContainerHighest).Children(func() {
		ui.Row(c).Gap(2).Fill().Children(func() {
			for _, sub := range []struct{ key, label string }{
				{"recent", fmt.Sprintf("最近使用 · %d", len(a.recent))},
				{"favorites", fmt.Sprintf("我的收藏 · %d", len(a.favorites))},
			} {
				sub := sub
				isActive := a.localSubTab == sub.key
				btn := ui.ButtonBase(c.Key("sub-" + sub.key)).Grow(1).Height(32).Radius(14).Center()
				if isActive {
					btn.Background(scheme.SecondaryContainer)
				}
				btn.Children(func() {
					col := scheme.OnSurfaceVariant
					if isActive {
						col = scheme.OnSecondaryContainer
					}
					ui.Text(c, sub.label).FontSize(11).Bold().TextColor(col)
				})
				if btn.Clicked() {
					a.localSubTab = sub.key
				}
			}
		})
	})

	currentList := a.recent
	emptyText := "还没有使用过壁纸"
	listTitle := fmt.Sprintf("最近使用 · %d", len(a.recent))
	if a.localSubTab == "favorites" {
		currentList = a.favorites
		emptyText = "还没有收藏的壁纸\n在大图预览点击爱心可添加收藏"
		listTitle = fmt.Sprintf("我的收藏 · %d", len(a.favorites))
	}

	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).FillWidth().Children(func() {
		ui.Text(c, listTitle).FontSize(11).Bold().TextColor(scheme.OnSurfaceVariant)
		if a.localSubTab != "favorites" && len(a.recent) > 0 {
			clear := mayu.TextButton(c, "清空").OnClick(a.clearRecent)
			clear.Height(28)
		}
	})

	ui.Scroll(c).Grow(1).Gap(6).Children(func() {
		if len(currentList) == 0 {
			ui.Text(c, emptyText).FontSize(12).TextColor(scheme.OnSurfaceVariant)
		}
		for _, path := range currentList {
			path := path
			isSelected := a.selected == path
			spec := a.getWallpaperSpec(path)
			bg := ui.Transparent
			if isSelected {
				bg = scheme.SecondaryContainer
			}
			item := mayu.ListItem(c, filepath.Base(path), mayu.ListItemOptions{
				SupportingText: spec,
				OnClick:        func() { a.loadWallpaper(path) },
				Trailing: func() {
					if isSelected {
						ui.Box(c).Size(22, 22).Radius(11).Background(scheme.PrimaryContainer).Center().Children(func() {
							ui.Icon(c, icons.Check).Size(13, 13).TextColor(scheme.OnPrimaryContainer)
						})
					}
				},
			})
			item.Background(bg).Radius(12)
		}
	})
}

// drawOnlineContext hosts the online category list.
func (a *application) drawOnlineContext(c *ui.Context, scheme theme.ColorScheme) {
	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).FillWidth().Children(func() {
		ui.Text(c, "壁纸分类").FontSize(13).Bold().TextColor(scheme.OnSurface)
		refresh := mayu.TextButton(c, "刷新").OnClick(a.loadOnlineCategories)
		refresh.Height(28)
	})

	ui.Scroll(c).Grow(1).Gap(6).Children(func() {
		if len(a.categories) == 0 {
			msg := "暂无分类数据"
			if a.loadingOnline {
				msg = "正在加载分类…"
			}
			ui.Text(c, msg).FontSize(12).TextColor(scheme.OnSurfaceVariant)
		}
		for _, cat := range a.categories {
			cat := cat
			isSelected := a.selectedCategory.ID == cat.ID && a.selectedCategory.Source == cat.Source
			btn := ui.ButtonBase(c.Key("cat-"+cat.Source+"-"+cat.ID)).FillWidth().Height(40).Radius(12).Padding(0, 10).AlignItems(ui.Center)
			if isSelected {
				btn.Background(scheme.SecondaryContainer)
			}
			btn.Children(func() {
				col := scheme.OnSurface
				if isSelected {
					col = scheme.OnSecondaryContainer
				}
				ui.Row(c).AlignItems(ui.Center).Gap(10).Fill().Children(func() {
					ui.Icon(c, icons.ForCategory(cat.Name)).Size(16, 16).TextColor(col)
					ui.Text(c, cat.Name).FontSize(12).Bold().TextColor(col).SingleLine()
					ui.Spacer(c)
					ui.Text(c, categoryBadge(cat)).FontSize(10).TextColor(col)
				})
			})
			if btn.Clicked() {
				a.onlinePage = 1
				a.selectedCategory = cat
				a.onlineView = "grid"
				a.loadOnlineWallpapers(cat, 1)
			}
		}
	})
}

// drawMainArea 绘制右侧主区域
func (a *application) drawMainArea(c *ui.Context, t *ui.Theme) {
	if a.tab == "plugins" {
		a.drawPluginManager(c, t)
	} else if a.tab == "online" {
		a.drawOnlineMain(c, t)
	} else {
		a.drawLocalMain(c, t)
	}
}

// drawLocalMain renders the local wallpaper workspace with M3 surfaces.
func (a *application) drawLocalMain(c *ui.Context, t *ui.Theme) {
	scheme := m3Scheme(c)
	ui.Column(c).Grow(1).FillHeight().Padding(18, 22, 18, 22).Gap(14).Background(scheme.Surface).Children(func() {
		ui.Row(c).AlignItems(ui.Center).FillWidth().DragWindow().Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, "桌面壁纸预览").FontSize(18).Bold().TextColor(scheme.OnSurface)
				ui.Text(c, selectedLabel(a.selected)).FontSize(12).TextColor(scheme.OnSurfaceVariant).MaxWidth(480).SingleLine()
			})
			ui.Spacer(c)
			a.drawFitSelector(c, t)
		})

		// 2. Image-first preview card (clean preview, no decorative desktop Dock).
		a.drawLargePreviewBox(c, t)

		// Action bar: show in folder, VS Code sync, apply to desktop.
		a.drawLocalBottomBar(c, t)
	})
}

// drawOnlineMain 绘制在线壁纸主页面
func (a *application) drawOnlineMain(c *ui.Context, t *ui.Theme) {
	scheme := m3Scheme(c)
	ui.Column(c).Grow(1).FillHeight().Padding(18, 22, 18, 22).Gap(14).Background(scheme.Surface).Children(func() {
		// 1. 顶部控制栏：分类名、副标题、搜索框、筛选下拉 (两端对齐，标题不截断)
		ui.Row(c).Justify(ui.SpaceBetween).AlignItems(ui.Center).FillWidth().DragWindow().Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				catName := a.selectedCategory.Name
				if catName == "" {
					catName = "必应每日精选"
				}
				sourceText := a.selectedCategory.Description
				if sourceText == "" {
					// Compatibility for callers that still construct the legacy
					// Category shape without plugin-provided presentation metadata.
					if a.selectedCategory.Source == "bing" {
						sourceText = "微软必应官方精选 · 每日自动同步 · 4K 超高清原图直连"
					} else if a.selectedCategory.Source == "360" {
						sourceText = "360壁纸库精选 · 国内 CDN 极速直连 · 高清多分辨率适配"
					} else {
						sourceText = "壁纸插件源 · 由插件提供分类、列表和图片地址"
					}
				}
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					if a.onlineView == "preview" {
						backBtn := ui.ButtonBase(c.Key("back-to-grid")).Padding(3, 8).Radius(6).Background(scheme.SurfaceContainerHighest)
						backBtn.Children(func() {
							ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
								ui.Icon(c, icons.ChevronLeft).Size(12, 12).TextColor(scheme.OnSurfaceVariant)
								ui.Text(c, "◀ 返回列表").FontSize(11).TextColor(scheme.OnSurfaceVariant)
							})
						})
						if backBtn.Clicked() {
							a.onlineView = "grid"
						}
					}
					ui.Text(c, "在线壁纸 · "+catName).FontSize(18).Bold().TextColor(scheme.OnSurface).SingleLine()
				})
				ui.Text(c, sourceText).FontSize(11).TextColor(scheme.OnSurfaceVariant).SingleLine()
			})

			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				// M3 search bar and select, themed by the active Material You scheme.
				mayu.SearchBar(c, &a.searchKeyword, "搜索壁纸关键词…").Width(200)
				mayu.Select(c, &a.filterOption, "", []components.SelectOption[string]{
					{Value: "精选推荐", Label: "精选推荐"},
					{Value: "最新壁纸", Label: "最新壁纸"},
					{Value: "最高人气", Label: "最高人气"},
				}).Width(150)

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
	scheme := m3Scheme(c)
	ui.Box(c).Grow(1).FillWidth().MinHeight(260).Radius(16).Clip().Background(scheme.SurfaceContainerLow).Border(1, scheme.OutlineVariant).Padding(12, 12).Children(func() {
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
					borderColor := scheme.OutlineVariant
					if isSelected {
						borderColor = scheme.Primary
					}

					ui.Column(c.Key("card-"+item.ID)).Padding(8, 8).Gap(8).Radius(10).Background(scheme.Surface).Border(1, borderColor).Children(func() {
						// 缩略图盒子 + 左上角 4K UHD 胶囊徽标
						ui.Box(c).Height(118).FillWidth().Radius(6).Clip().Children(func() {
							a.thumbMu.Lock()
							bmp := a.thumbBitmaps[item.ID]
							a.thumbMu.Unlock()

							if bmp != nil {
								ui.Image(c, bmp).Fill().Radius(6).Fit(ui.Cover)
							} else {
								a.requestThumbnail(item)
								ui.Box(c).Fill().Background(scheme.OutlineVariant).Center().Children(func() {
									ui.Text(c, "加载中…").FontSize(11).TextColor(scheme.OnSurfaceVariant)
								})
							}

							resBadge := "4K UHD"
							if item.Resolution != "" && !strings.Contains(item.Resolution, "3840") && !strings.Contains(item.Resolution, "4K") {
								resBadge = item.Resolution
							}
							// 顶层悬浮 Badge (Absolute 左上角)
							ui.Box(c).Absolute().Top(8).Left(8).Padding(2, 6).Radius(4).Background(ui.Hex("#00000070")).Children(func() {
								ui.Text(c, resBadge).FontSize(9).Bold().TextColor(scheme.Surface)
							})
						})

						// 标题单行加粗 (符合原型设计)
						ui.Row(c).AlignItems(ui.Center).FillWidth().Children(func() {
							ui.Text(c, item.Title).FontSize(12).Bold().TextColor(scheme.OnSurface).Grow(1).SingleLine()
						})

						// 卡片操作按钮行：↗ 预览 / ✓ 应用 (等宽双按钮，对齐原型)
						ui.Row(c).Gap(6).FillWidth().Children(func() {
							prevBtn := ui.ButtonBase(c.Key("btn-prev-"+item.ID)).Grow(1).Height(28).Radius(6).Background(scheme.SurfaceContainerHighest).Border(1, scheme.Outline)
							prevBtn.Children(func() {
								ui.Row(c).Center().Gap(4).Children(func() {
									ui.Icon(c, icons.Eye).Size(12, 12).TextColor(scheme.OnSurfaceVariant)
									ui.Text(c, "↗ 预览").FontSize(11).TextColor(scheme.OnSurfaceVariant)
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
								applyBtn.Background(scheme.OnSurfaceVariant)
							} else {
								applyBtn.Background(scheme.Primary)
							}
							applyBtn.Children(func() {
								ui.Row(c).Center().Gap(4).Children(func() {
									if !(a.busy && isSelected) {
										ui.Icon(c, icons.Check).Size(12, 12).TextColor(scheme.Surface)
									}
									ui.Text(c, btnLabel).FontSize(11).Bold().TextColor(scheme.Surface)
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

// drawLargePreviewBox 绘制大图预览区域
func (a *application) drawLargePreviewBox(c *ui.Context, t *ui.Theme) {
	scheme := m3Scheme(c)
	// The preview stage stays a deep neutral in BOTH appearances so wallpaper
	// colors read true and the dark immersive gallery stays consistent. Using
	// InverseSurface here would flip the stage to light in dark mode.
	stageBg := ui.Hex("#1C1B1F")
	stageFg := ui.Hex("#E6E1E5")
	ui.Box(c).Grow(1).FillWidth().MinHeight(320).Radius(20).Clip().Background(stageBg).Children(func() {
		if a.preview == nil {
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Text(c, "还没有选择壁纸").FontSize(18).Bold().TextColor(stageFg)
				ui.Text(c, "支持 JPG、PNG、WebP 和 BMP").FontSize(12).TextColor(stageFg.Alpha(0.7))
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
		dotColor := scheme.Primary
		statusBadge := "预览模式 (未应用)"
		if isApplied {
			dotColor = scheme.Tertiary
			statusBadge = "当前系统桌面壁纸"
		}

		ui.Row(c).Absolute().Top(16).Left(16).Right(16).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			// 左侧桌面状态胶囊
			ui.Box(c).Padding(5, 12).Radius(20).Background(ui.Hex("#00000070")).Border(1, ui.Hex("#FFFFFF20")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Box(c).Width(7).Height(7).Radius(4).Background(dotColor)
					ui.Text(c, statusBadge).FontSize(11).Bold().TextColor(scheme.Surface)
				})
			})

			// 右侧工具胶囊群：规格 + 收藏 + 另存为
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				// 1. 规格信息胶囊
				ui.Box(c).Padding(4, 10).Radius(20).Background(ui.Hex("#00000060")).Children(func() {
					ui.Text(c, resolutionText).FontSize(11).TextColor(scheme.OutlineVariant)
				})

				// 2. 收藏胶囊按钮
				isFav := a.IsFavorite(a.selected)
				favBg := ui.Hex("#00000070")
				favBorder := ui.Hex("#FFFFFF30")
				favTextCol := scheme.Surface
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
							ui.Icon(c, icons.Heart).Size(12, 12).TextColor(scheme.Surface)
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
						ui.Icon(c, icons.Download).Size(12, 12).TextColor(scheme.Surface)
						ui.Text(c, "另存为").FontSize(11).TextColor(scheme.Surface)
					})
				})
				if saveBtn.Clicked() {
					a.exportWallpaper()
				}
			})
		})

	})
}

// drawFitSelector draws the wallpaper fit mode as a single M3 segmented
// button: one outlined track with three connected segments. The glyph-prefixed
// strings live in Label() (accessibility + test contract) while the visible
// face is clean text, so the control no longer looks like three loose pills.
func (a *application) drawFitSelector(c *ui.Context, t *ui.Theme) {
	scheme := m3Scheme(c)
	options := []struct {
		val   string
		aria  string
		label string
	}{
		{"覆盖", "⬒ 覆盖", "覆盖"},
		{"适应", "⬓ 适应", "适应"},
		{"拉伸", "⬖ 拉伸", "拉伸"},
	}
	ui.Box(c).Height(36).Padding(3).Radius(18).Background(scheme.SurfaceContainerHighest).Border(1, scheme.OutlineVariant).Clip().Children(func() {
		ui.Row(c).FillHeight().Children(func() {
			for i, opt := range options {
				opt := opt
				isSelected := a.fit == opt.val
				segment := ui.ButtonBase(c.Key("seg-" + opt.val)).Width(58).FillHeight().Label(opt.aria)
				if i > 0 {
					segment.BorderWidth(0, 0, 0, 1).BorderColor(scheme.OutlineVariant)
				}
				if isSelected {
					segment.Background(scheme.SecondaryContainer).Radius(15)
				}
				segment.Center().Children(func() {
					col := scheme.OnSurfaceVariant
					if isSelected {
						col = scheme.OnSecondaryContainer
					}
					ui.Text(c, opt.label).FontSize(12).Bold().TextColor(col).SingleLine()
				})
				if segment.Clicked() {
					a.fit = opt.val
				}
			}
		})
	})
}

// drawActionButton renders a labeled action button: icon and caption on the
// same row so every operation is self-explanatory without hovering a tooltip.
// active gives the "VS Code 自动同步" toggle its on-state highlight.
func drawActionButton(c *ui.Context, scheme theme.ColorScheme, key, label string, icon *ui.SVG, disabled, active bool) ui.Element {
	bg := scheme.SurfaceContainerLow
	fg := scheme.OnSurface
	border := scheme.OutlineVariant
	if active {
		bg = scheme.SecondaryContainer
		fg = scheme.OnSecondaryContainer
		border = scheme.Outline
	}
	btn := ui.ButtonBase(c.Key(key)).Height(38).Padding(0, 12).Radius(19).
		Background(bg).Border(1, border).Disabled(disabled).Label(label).Tooltip(label)
	if !disabled && btn.Hovered() {
		btn.Background(scheme.SurfaceContainerHigh)
	}
	btn.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Icon(c, icon).Size(15, 15).TextColor(fg)
			ui.Text(c, label).FontSize(12).Bold().TextColor(fg).SingleLine()
		})
	})
	return btn
}

// drawLocalBottomBar 绘制本地壁纸底部操作栏
func (a *application) drawLocalBottomBar(c *ui.Context, t *ui.Theme) {
	scheme := m3Scheme(c)
	ui.Column(c).Gap(10).FillWidth().Children(func() {
		// Status line on its own row so it never competes with the actions.
		dotColor := scheme.Tertiary
		if a.error {
			dotColor = scheme.Error
		} else if a.busy {
			dotColor = scheme.Primary
		}
		statusMsg := "已成功应用到桌面并同步"
		if a.status != "" {
			statusMsg = a.status
		}
		ui.Row(c).AlignItems(ui.Center).Gap(7).FillWidth().Children(func() {
			ui.Box(c).Width(8).Height(8).Radius(4).Background(dotColor)
			ui.Text(c, statusMsg).FontSize(12).TextColor(scheme.OnSurfaceVariant).SingleLine()
		})

		// Action row: secondary actions on the left, primary apply on the right.
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(10).FillWidth().Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				folderBtn := drawActionButton(c, scheme, "btn-show-folder", "在文件夹中显示", icons.FolderOpen, a.selected == "", false)
				if folderBtn.Clicked() {
					mygo.Shell.ShowItemInFolder(a.selected)
				}

				vscodeBtn := drawActionButton(c, scheme, "btn-map-vscode", "映射到 VS Code", icons.Code, a.selected == "" || a.busy, false)
				if vscodeBtn.Clicked() {
					a.applyVSCode()
				}

				syncLabel := "VS Code 自动同步"
				if a.syncVSCode {
					syncLabel += " · 开"
				} else {
					syncLabel += " · 关"
				}
				syncBtn := drawActionButton(c, scheme, "btn-sync-vscode", syncLabel, icons.Refresh, false, a.syncVSCode)
				if syncBtn.Clicked() {
					a.syncVSCode = !a.syncVSCode
					a.toggleSyncVSCode()
				}
			})

			applyDisabled := a.selected == "" || a.busy
			applyText := "应用到桌面"
			if a.selected == "" {
				applyText = "请选择壁纸"
			}
			applyBg := scheme.Primary
			applyFg := scheme.OnPrimary
			if applyDisabled {
				applyBg = scheme.SurfaceContainerHighest
				applyFg = scheme.OnSurfaceVariant
			}
			applyBtn := ui.ButtonBase(c.Key("btn-apply-desktop")).Height(42).Padding(0, 20).Radius(21).
				Background(applyBg).Disabled(applyDisabled).Label("应用到桌面").Tooltip("应用到桌面")
			applyBtn.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Icon(c, icons.Check).Size(16, 16).TextColor(applyFg)
					ui.Text(c, applyText).FontSize(13).Bold().TextColor(applyFg).SingleLine()
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
	scheme := m3Scheme(c)
	// 在线大图预览态复用底部操作栏，支持直接设为壁纸与映射到 VS Code
	if a.onlineView == "preview" {
		a.drawLocalBottomBar(c, t)
		return
	}

	ui.Row(c).AlignItems(ui.Center).Gap(10).FillWidth().Children(func() {
		// 左侧：状态圆点与当前状态
		statusMsg := fmt.Sprintf("已拉取 %s · 共 %d 张壁纸", a.selectedCategory.Name, len(a.wallpapers))
		if a.status != "" && (a.error || a.busy) {
			statusMsg = a.status
		}

		dotColor := scheme.Tertiary
		if a.error {
			dotColor = scheme.Error
		} else if a.busy || a.loadingOnline {
			dotColor = scheme.Primary
		}

		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Box(c).Width(7).Height(7).Radius(4).Background(dotColor)
			ui.Text(c, statusMsg).FontSize(12).TextColor(scheme.OnSurfaceVariant).SingleLine()
		})

		ui.Spacer(c)

		// 右侧：分页器群 (Pagination Cluster)
		currentPage := a.onlinePage
		if currentPage < 1 {
			currentPage = 1
		}
		pageSize := 18
		totalPages := 0
		if a.onlineTotal > 0 {
			totalPages = (a.onlineTotal + pageSize - 1) / pageSize
		}
		if totalPages == 0 {
			// Legacy callers may render a category before its first page has
			// returned; retain the old preview text until plugin metadata arrives.
			switch a.selectedCategory.Source {
			case "bing":
				totalPages = 1
			case "360":
				totalPages = 10
			default:
				totalPages = 3
			}
		}

		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			prevBtn := ui.ButtonBase(c.Key("page-prev")).Height(30).Padding(0, 10).Radius(6).Background(scheme.SurfaceContainerLow).Border(1, scheme.OutlineVariant).Disabled(currentPage <= 1 || a.loadingOnline)
			prevBtn.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
					ui.Icon(c, icons.ChevronLeft).Size(11, 11).TextColor(scheme.OnSurfaceVariant)
					ui.Text(c, "◀ 上一页").FontSize(11).TextColor(scheme.OnSurfaceVariant)
				})
			})
			if prevBtn.Clicked() && currentPage > 1 {
				a.onlinePage--
				a.loadOnlineWallpapers(a.selectedCategory, a.onlinePage)
			}

			ui.Box(c).Padding(0, 8).Children(func() {
				pageLabel := fmt.Sprintf("第 %d 页", currentPage)
				if totalPages > 0 {
					pageLabel = fmt.Sprintf("第 %d / %d 页", currentPage, totalPages)
				}
				ui.Text(c, pageLabel).FontSize(11).Bold().TextColor(scheme.OnSurface)
			})

			nextBtn := ui.ButtonBase(c.Key("page-next")).Height(30).Padding(0, 10).Radius(6).Background(scheme.Surface).Border(1, scheme.OutlineVariant).Disabled(!a.onlineMore || a.loadingOnline)
			nextBtn.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
					ui.Text(c, "下一页 ▶").FontSize(11).TextColor(scheme.OnSurface)
					ui.Icon(c, icons.ChevronRight).Size(11, 11).TextColor(scheme.OnSurface)
				})
			})
			if nextBtn.Clicked() && a.onlineMore {
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
	if cat.Badge != "" {
		return cat.Badge
	}
	// Keep the old display for legacy categories created by older callers.
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
			a.loadWallpaper(paths[0])
		}
	}()
}

func (a *application) loadWallpaper(path string) {
	seq := a.loadSeq.Add(1)
	a.update(func() {
		a.busy = true
		a.setStatus("正在读取图片…", false)
	})
	go func() {
		data, err := os.ReadFile(path)
		readErr := err
		if err == nil {
			preview, decodeErr := decodeWallpaper(data)
			err = decodeErr
			if err == nil {
				a.update(func() {
					if seq != a.loadSeq.Load() {
						return
					}
					a.selected, a.preview = path, preview
					a.thumbMu.Lock()
					a.recentBitmaps[path] = preview
					a.thumbMu.Unlock()
					a.busy = false
					a.setStatus("准备应用到桌面", false)
				})
				return
			}
		}
		a.update(func() {
			if seq != a.loadSeq.Load() {
				return
			}
			a.busy = false
			if readErr != nil {
				a.setStatus("读取图片失败: "+readErr.Error(), true)
			} else if err != nil {
				a.setStatus("无法预览这张图片: "+err.Error(), true)
			}
		})
	}()
}

func decodeWallpaper(data []byte) (*ui.Bitmap, error) {
	if int64(len(data)) > maxWallpaperFileBytes {
		return nil, fmt.Errorf("图片文件过大，不能超过 %d MB", maxWallpaperFileBytes>>20)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("读取图片尺寸失败: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 {
		return nil, fmt.Errorf("图片尺寸无效")
	}
	if int64(config.Width)*int64(config.Height) > maxWallpaperPixels {
		return nil, fmt.Errorf("图片像素过多，不能超过 %d 像素", maxWallpaperPixels)
	}
	return ui.DecodeBitmap(data)
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
		result, err := a.onlineSvc.GetWallpaperPage(context.Background(), cat, page, pageSize)
		a.update(func() {
			a.loadingOnline = false
			if err != nil {
				a.status = "拉取壁纸失败: " + err.Error()
				a.error = true
				return
			}
			a.wallpapers = result.Items
			a.onlineMore = result.More
			a.onlineTotal = result.Total
			if len(result.Items) > 0 {
				a.status = fmt.Sprintf("%s · 第 %d 页，共加载 %d 张", cat.Name, page, len(result.Items))
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
			bmp, _ = decodeWallpaper(data)
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
			bmp, _ = decodeWallpaper(data)
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
			bmp, err := decodeWallpaper(data)
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

// prepareForRelaunch prevents the tray close handler from hiding the window
// while MyGo runs its normal quit-and-restart sequence.
func (a *application) prepareForRelaunch() {
	a.quitting = true
	if a.tray != nil {
		a.tray.Destroy()
		a.tray = nil
	}
}

func (a *application) relaunchAfterUpdate() {
	mygo.RunOnMain(func() {
		a.prepareForRelaunch()
		mygo.App.Relaunch()
	})
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
			pending := errors.Is(err, updater.ErrInstallPending)
			a.update(func() {
				if pending {
					a.setStatus("系统安装器已打开，请完成安装后重新启动应用", false)
				} else {
					a.setStatus("自动安装未完成，已保存安装包", true)
				}
			})
			if a.window != nil {
				_, _ = mygo.Dialog.Message(mygo.MessageOptions{
					Parent: a.window,
					Type:   mygo.MessageWarning,
					Title:  "更新安装提示",
					Message: func() string {
						if pending {
							return "系统安装器已打开，但 BGGo 无法确认安装是否完成"
						}
						return "未能完成自动提权安装"
					}(),
					Detail:  fmt.Sprintf("更新包已保存至:\n%s\n\n请完成系统安装器中的操作；若安装器未成功，可在终端执行:\nsudo dpkg -i %s\n\n安装完成后请重新启动 BGGo。", destFile, destFile),
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
				a.relaunchAfterUpdate()
			}
		}
		return
	}

	// Writable portable installs use MyGo's installer so the signed archive is
	// installed before Relaunch. Merely downloading an archive cannot update the
	// running installation.
	if !mygo.Updater.Enabled() {
		a.update(func() {
			a.setStatus("当前安装方式不支持自动更新，请从发布页下载安装包", true)
		})
		return
	}

	nativeUpdate, err := mygo.Updater.Check(ctx)
	if err != nil {
		a.update(func() {
			a.setStatus("检查原生更新失败: "+err.Error(), true)
		})
		return
	}
	if nativeUpdate == nil {
		a.update(func() {
			a.setStatus("当前已是最新版本", false)
		})
		return
	}

	err = nativeUpdate.Install(ctx, func(downloaded, total int64) {
		if total > 0 {
			pct := int(downloaded * 100 / total)
			a.update(func() {
				a.setStatus(fmt.Sprintf("正在下载更新包… %d%%", pct), false)
			})
		}
	})
	if err != nil {
		a.update(func() {
			a.setStatus("安装更新失败: "+err.Error(), true)
		})
		return
	}

	a.update(func() {
		a.setStatus("更新安装完成，正在重启应用…", false)
	})
	a.relaunchAfterUpdate()
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
	var items []online.WallpaperItem
	var err error
	if provider, ok := a.onlineSvc.Provider("bing"); ok {
		if pluginProvider, ok := provider.(*online.PluginProvider); ok {
			items, err = pluginProvider.Daily(context.Background())
		} else if a.onlineSvc.Bing != nil {
			items, err = a.onlineSvc.Bing.Fetch(context.Background(), "bing_daily", 1, 1)
		}
	} else {
		err = fmt.Errorf("未找到 Bing 壁纸源")
	}
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
		bmp, _ = decodeWallpaper(data)
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
		var items []online.WallpaperItem
		var err error
		if provider, ok := a.onlineSvc.Provider("360"); ok {
			if pluginProvider, ok := provider.(*online.PluginProvider); ok {
				items, err = pluginProvider.Random(context.Background(), "9", 30)
			} else if a.onlineSvc.Qihoo != nil {
				items, err = a.onlineSvc.Qihoo.Fetch(context.Background(), "9", 1, 30)
			}
		} else {
			err = fmt.Errorf("未找到随机壁纸源")
		}
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
					a.update(func() { a.tab = "local" })
					a.loadWallpaper(p)
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
			for _, source := range a.plugins {
				source.Close()
			}
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
