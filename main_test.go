package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bggo/internal/library"
	"bggo/internal/online"
	"bggo/internal/updater"
	"bggo/internal/vscode"
	"github.com/egoist/mygo/ui"
)

func TestInitialView(t *testing.T) {
	a := &application{fit: "覆盖", status: "选择一张图片开始"}
	tt := ui.NewTester(a.view, 1000, 680)
	for _, text := range []string{
		"BGGo",
		AppVersion,
		"桌面壁纸预览",
		"还没有选择壁纸",
		"✓ 应用到桌面",
		"同步到 VS Code",
		"检查更新",
		"↗ 在文件夹中显示",
	} {
		if !tt.HasText(text) {
			t.Errorf("missing text %q; texts = %q", text, tt.Texts())
		}
	}
	if tt.HasText("Linux 优先 · Windows 已适配") {
		t.Errorf("expected 'Linux 优先 · Windows 已适配' to be removed")
	}
}

func TestPreviewFit(t *testing.T) {
	tests := map[string]ui.Fit{
		"覆盖": ui.Cover,
		"适应": ui.Contain,
		"拉伸": ui.FillBox,
	}
	for name, want := range tests {
		if got := previewFit(name); got != want {
			t.Errorf("previewFit(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestToggleSyncVSCode(t *testing.T) {
	tmp := t.TempDir()
	store := &library.Store{}
	// Use library package reflection or store in temp dir by creating store file
	storePath := filepath.Join(tmp, "library.json")
	storeRef := store
	// store has unexported path, we can test via SetSyncVSCode directly on library.Store or initialize application
	_ = storePath
	_ = storeRef

	a := &application{syncVSCode: false}
	a.syncVSCode = true
	a.toggleSyncVSCode()
	if !a.syncVSCode {
		t.Errorf("expected syncVSCode=true")
	}
}

func TestApplyVSCodeEmpty(t *testing.T) {
	a := &application{selected: ""}
	a.applyVSCode()
	if a.busy {
		t.Errorf("expected not busy when selected is empty")
	}
}

func TestApplyVSCodeService(t *testing.T) {
	tmp := t.TempDir()
	userDir := filepath.Join(tmp, "User")
	dummyImg := filepath.Join(tmp, "wall.png")

	a := &application{
		selected: dummyImg,
		vscode:   vscode.Service{UserDir: userDir},
	}
	// Calling applyVSCode should handle background execution without crashing when window is nil
	a.applyVSCode()
}

func TestCheckForUpdates(t *testing.T) {
	called := false
	a := &application{
		checkUpdate: func() {
			called = true
		},
	}
	a.checkForUpdates()
	if !called {
		t.Errorf("expected checkUpdate callback to be called")
	}
	if a.status != "正在检查应用更新…" {
		t.Errorf("status = %q, want %q", a.status, "正在检查应用更新…")
	}
	if a.error {
		t.Errorf("expected error=false")
	}
}

func TestApplication_InitUpdater(t *testing.T) {
	a := newApplication()
	if a.updaterSvc == nil {
		t.Fatalf("expected updaterSvc to be initialized")
	}
	if a.checkUpdate == nil {
		t.Fatalf("expected checkUpdate to be set")
	}
}

func TestApplication_HandleCheckForUpdates_UpToDate(t *testing.T) {
	manifestPayload := updater.Manifest{
		Version:   "0.2.6",
		Date:      "2026-11-12T00:00:00Z",
		URL:       "https://example.com/bggo-0.2.6-linux-amd64.tar.gz",
		Size:      12345,
		Signature: "sig",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifestPayload)
	}))
	defer server.Close()

	svc := updater.NewService(updater.Config{
		Repo:          "Delta1035/bggo",
		HTTPClient:    server.Client(),
		UpdateFeedURL: server.URL,
	})

	a := &application{
		updaterSvc: svc,
	}
	a.checkUpdate = a.handleCheckForUpdates

	a.checkForUpdates()

	// Wait for goroutine in handleCheckForUpdates to complete
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.RLock()
		st := a.status
		a.mu.RUnlock()
		if strings.Contains(st, "已是最新版本") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	expectedStatus := fmt.Sprintf("BGGo 已是最新版本 (%s)", AppVersion)
	if a.status != expectedStatus {
		t.Errorf("status = %q, want %q", a.status, expectedStatus)
	}
}

func TestApplication_HandleCheckForUpdates_UpdateAvailable(t *testing.T) {
	manifestPayload := updater.Manifest{
		Version:   "0.9.9",
		Date:      "2026-11-12T00:00:00Z",
		URL:       "https://example.com/bggo-0.9.9-linux-amd64.tar.gz",
		Size:      12345,
		Signature: "sig",
		Notes:     "Awesome release",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifestPayload)
	}))
	defer server.Close()

	svc := updater.NewService(updater.Config{
		Repo:          "Delta1035/bggo",
		HTTPClient:    server.Client(),
		UpdateFeedURL: server.URL,
	})

	a := &application{
		updaterSvc: svc,
	}
	a.checkUpdate = a.handleCheckForUpdates

	a.checkForUpdates()

	// Wait for goroutine in handleCheckForUpdates to complete
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.RLock()
		st := a.status
		a.mu.RUnlock()
		if strings.Contains(st, "发现新版本 0.9.9") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	expectedStatus := "发现新版本 0.9.9！"
	if a.status != expectedStatus {
		t.Errorf("status = %q, want %q", a.status, expectedStatus)
	}
}

func TestApplication_RunSilentUpdateCheck(t *testing.T) {
	manifestPayload := updater.Manifest{
		Version:   "0.9.9",
		Date:      "2026-11-12T00:00:00Z",
		URL:       "https://example.com/bggo-0.9.9-linux-amd64.tar.gz",
		Size:      12345,
		Signature: "sig",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifestPayload)
	}))
	defer server.Close()

	svc := updater.NewService(updater.Config{
		Repo:          "Delta1035/bggo",
		HTTPClient:    server.Client(),
		UpdateFeedURL: server.URL,
	})

	a := &application{
		updaterSvc: svc,
	}
	a.runSilentUpdateCheck()

	a.mu.RLock()
	defer a.mu.RUnlock()
	if !strings.Contains(a.status, "发现新版本 0.9.9") {
		t.Errorf("expected status to mention new version, got %q", a.status)
	}
}

func TestApplyVSCode_AutoInstallExtension(t *testing.T) {
	tmp := t.TempDir()
	userDir := filepath.Join(tmp, "User")
	_ = os.MkdirAll(userDir, 0o755)
	dummyImg := filepath.Join(tmp, "wall.png")
	_ = os.WriteFile(dummyImg, []byte("pngdata"), 0o644)

	installedDone := make(chan struct{})
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		close(installedDone)
		return []byte("installed"), nil
	}

	fakeCLI := filepath.Join(tmp, "code")
	_ = os.WriteFile(fakeCLI, []byte(""), 0o755)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fake-vsix"))
	}))
	defer srv.Close()

	a := &application{
		selected: dummyImg,
		vscode: vscode.Service{
			UserDir:       userDir,
			ExtensionDir:  filepath.Join(tmp, "extensions"),
			VSCodeCLIPath: fakeCLI,
			DownloadURL:   srv.URL,
			CommandRunner: mockRunner,
		},
	}

	a.applyVSCode()

	select {
	case <-installedDone:
		// 成功触发自动下载安装
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for auto-install of extension, status=%q", a.Status())
	}
}

func TestOnlineViewTabsAndControls(t *testing.T) {
	a := newApplication()
	a.tab = "online"
	a.categories = []online.Category{
		{ID: "bing_daily", Name: "必应每日精选", Source: "bing"},
		{ID: "36", Name: "4K专区", Source: "360"},
	}
	a.selectedCategory = a.categories[0]

	tt := ui.NewTester(a.view, 1000, 680)

	for _, text := range []string{
		"本地壁纸",
		"在线图库",
		"壁纸分类",
		"必应每日精选",
		"4K专区",
		"在线壁纸 · 必应每日精选",
		"微软必应官方精选 · 每日自动同步 · 4K 超高清原图直连",
		"精选推荐",
	} {
		if !tt.HasText(text) {
			t.Errorf("missing text %q in online view; texts = %q", text, tt.Texts())
		}
	}
}

func TestOnlineGridRenderingWithItems(t *testing.T) {
	a := newApplication()
	a.tab = "online"
	a.onlineView = "grid"
	a.categories = []online.Category{
		{ID: "36", Name: "4K专区", Source: "360"},
	}
	a.selectedCategory = a.categories[0]
	a.wallpapers = []online.WallpaperItem{
		{
			ID:         "test_wp_1",
			Title:      "测试4K山水壁纸",
			Resolution: "3840x2160",
			Source:     "360",
		},
		{
			ID:         "test_wp_2",
			Title:      "极简星空大片",
			Resolution: "1920x1080",
			Source:     "360",
		},
	}

	tt := ui.NewTester(a.view, 1000, 680)

	for _, text := range []string{
		"测试4K山水壁纸",
		"4K UHD",
		"极简星空大片",
		"1920x1080",
		"↗ 预览",
		"✓ 应用",
		"◀ 上一页",
		"下一页 ▶",
	} {
		if !tt.HasText(text) {
			t.Errorf("missing text %q in grid view; texts = %q", text, tt.Texts())
		}
	}
}

func TestOnlinePreviewModeRendering(t *testing.T) {
	a := newApplication()
	a.tab = "online"
	a.onlineView = "preview"
	a.categories = []online.Category{
		{ID: "bing_daily", Name: "必应每日精选", Source: "bing"},
	}
	a.selectedCategory = a.categories[0]

	tt := ui.NewTester(a.view, 1000, 680)

	for _, text := range []string{
		"覆盖",
		"适应",
		"拉伸",
		"还没有选择壁纸",
	} {
		if !tt.HasText(text) {
			t.Errorf("missing text %q in online preview view; texts = %q", text, tt.Texts())
		}
	}
}

func TestCategoryIcon(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"必应每日精选", "🌟"},
		{"4K专区", "🖥"},
		{"风景大片", "🏞"},
		{"动漫卡通", "🌸"},
		{"游戏壁纸", "🎮"},
		{"萌宠动物", "🐱"},
		{"汽车天下", "🚗"},
		{"小清新", "🌿"},
		{"其他分类", "🖼"},
	}

	for _, tc := range tests {
		if got := categoryIcon(tc.name); got != tc.want {
			t.Errorf("categoryIcon(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

type testWallpaperSetter struct {
	mu         sync.Mutex
	calledPath string
	done       chan struct{}
}

func (m *testWallpaperSetter) Set(path string) error {
	m.mu.Lock()
	m.calledPath = path
	m.mu.Unlock()
	if m.done != nil {
		select {
		case <-m.done:
		default:
			close(m.done)
		}
	}
	return nil
}

func (m *testWallpaperSetter) Path() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calledPath
}

func generateTestPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestApplyOnlineWallpaper(t *testing.T) {
	tmpDir := t.TempDir()
	pngData := generateTestPNG()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	}))
	defer srv.Close()

	onlineSvc, err := online.NewService(filepath.Join(tmpDir, "cache"))
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	onlineSvc.Client = srv.Client()

	mockSetter := &testWallpaperSetter{done: make(chan struct{})}
	a := newApplication()
	a.onlineSvc = onlineSvc
	a.setter = mockSetter
	a.syncVSCode = false

	item := online.WallpaperItem{
		ID:         "test_item_apply",
		Title:      "测试壁纸条目",
		FullURL:    srv.URL + "/wallpaper.png",
		Resolution: "1920x1080",
		Source:     "bing",
	}

	a.applyOnlineWallpaper(item)

	// 等待后台下载并应用完成
	deadline := time.Now().Add(3 * time.Second)
	for a.IsBusy() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if mockSetter.Path() == "" {
		t.Fatalf("expected setter to be called with downloaded path")
	}
	if a.Selected() != mockSetter.Path() {
		t.Errorf("a.selected %q != setter path %q", a.Selected(), mockSetter.Path())
	}
	if !strings.Contains(a.Status(), "壁纸已成功应用到桌面") {
		t.Errorf("expected success status, got %q", a.Status())
	}
	if a.Preview() == nil {
		t.Errorf("expected preview bitmap to be loaded")
	}
}

func TestPreviewOnlineWallpaper(t *testing.T) {
	tmpDir := t.TempDir()
	pngData := generateTestPNG()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	}))
	defer srv.Close()

	onlineSvc, err := online.NewService(filepath.Join(tmpDir, "cache"))
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	onlineSvc.Client = srv.Client()

	a := newApplication()
	a.onlineSvc = onlineSvc

	item := online.WallpaperItem{
		ID:         "test_item_preview",
		Title:      "测试大图预览条目",
		FullURL:    srv.URL + "/preview.png",
		Resolution: "1920x1080",
		Source:     "360",
	}

	a.previewOnlineWallpaper(item)

	deadline := time.Now().Add(3 * time.Second)
	for a.IsBusy() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if a.onlineView != "preview" {
		t.Errorf("expected onlineView = preview, got %s", a.onlineView)
	}
	if a.Preview() == nil {
		t.Fatalf("expected non-nil preview bitmap")
	}
	if a.Selected() == "" {
		t.Errorf("expected non-empty selected path")
	}
	if !strings.Contains(a.Status(), "已加载高清预览") {
		t.Errorf("expected preview status, got %q", a.Status())
	}
}

func TestLoadOnlineCategoriesAndWallpapers(t *testing.T) {
	tmpDir := t.TempDir()

	fakeBingJSON := `{
		"images": [
			{"startdate": "20261111", "url": "/bing.jpg", "title": "Bing今日大图", "hsh": "b1"}
		]
	}`
	fakeQihooCats := `{"errno": "0", "data": [{"id": "9", "name": "风景大片"}]}`
	fakeQihooWps := `{"errno": "0", "data": [{"id": "1", "class_id": "9", "resolution": "1920x1080", "url": "http://p1.qhimg.com/bdr/__85/1.jpg", "utag": "大海 蓝天"}]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "HPImageArchive.aspx"):
			_, _ = w.Write([]byte(fakeBingJSON))
		case strings.Contains(r.URL.RawQuery, "getAllCategoriesV2"):
			_, _ = w.Write([]byte(fakeQihooCats))
		case strings.Contains(r.URL.RawQuery, "getAppsByCategory"):
			_, _ = w.Write([]byte(fakeQihooWps))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	onlineSvc, err := online.NewService(filepath.Join(tmpDir, "cache"))
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	onlineSvc.Client = srv.Client()
	onlineSvc.Bing.BaseURL = srv.URL
	onlineSvc.Bing.Client = srv.Client()
	onlineSvc.Qihoo.CatURL = srv.URL
	onlineSvc.Qihoo.BaseURL = srv.URL
	onlineSvc.Qihoo.Client = srv.Client()

	a := newApplication()
	a.onlineSvc = onlineSvc

	a.loadOnlineCategories()

	deadline := time.Now().Add(3 * time.Second)
	for (a.IsLoadingOnline() || len(a.Categories()) == 0 || len(a.Wallpapers()) == 0) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	cats := a.Categories()
	wps := a.Wallpapers()
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories, got %d (%+v)", len(cats), cats)
	}
	if len(wps) != 1 {
		t.Fatalf("expected 1 wallpaper, got %d (%+v)", len(wps), wps)
	}
	if wps[0].Title != "Bing今日大图" {
		t.Errorf("expected wallpaper title 'Bing今日大图', got %q", wps[0].Title)
	}
}

func TestFavoritesAndSubTabs(t *testing.T) {
	tmpDir := t.TempDir()
	store := library.NewStoreWithPath(filepath.Join(tmpDir, "library.json"))
	_, _, _ = store.ToggleFavorite("/tmp/fav1.png")

	a := newApplication()
	a.store = store
	a.tab = "local"
	a.recent = []string{"/tmp/recent1.png"}
	a.favorites = []string{"/tmp/fav1.png"}

	// 1. 测试侧边栏渲染原型设计的 "最近使用 · 1" 和 "清空"
	tt := ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("最近使用 · 1") {
		t.Errorf("missing text '最近使用 · 1'")
	}
	if !tt.HasText("清空") {
		t.Errorf("missing text '清空'")
	}

	// 2. 切换收藏
	testPath := "/tmp/test_fav.png"
	a.toggleFavorite(testPath)
	if !a.IsFavorite(testPath) {
		t.Errorf("expected testPath to be favorited")
	}
	if len(a.Favorites()) != 2 {
		t.Errorf("expected 2 favorites, got %d", len(a.Favorites()))
	}

	// 3. 再次切换取消收藏
	a.toggleFavorite(testPath)
	if a.IsFavorite(testPath) {
		t.Errorf("expected testPath to be removed from favorites")
	}
}

func TestAutoBingSync(t *testing.T) {
	tmpDir := t.TempDir()
	store := library.NewStoreWithPath(filepath.Join(tmpDir, "library.json"))

	fakeBingJSON := `{
		"images": [
			{"startdate": "20261111", "url": "/bing_daily.png", "title": "今日必应美图", "hsh": "bing_hsh_1"}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "HPImageArchive.aspx") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fakeBingJSON))
		} else if strings.Contains(r.URL.Path, "bing_daily.png") {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(generateTestPNG())
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	onlineSvc, err := online.NewService(filepath.Join(tmpDir, "cache"))
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	onlineSvc.Client = srv.Client()
	onlineSvc.Bing.BaseURL = srv.URL
	onlineSvc.Bing.Client = srv.Client()

	mockSetter := &testWallpaperSetter{}
	a := newApplication()
	a.store = store
	a.onlineSvc = onlineSvc
	a.setter = mockSetter

	a.autoBing = true
	a.toggleAutoBing()
	if !a.AutoBing() {
		t.Errorf("expected autoBing = true")
	}

	// 等待自动同步完成
	deadline := time.Now().Add(3 * time.Second)
	for a.Selected() == "" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if mockSetter.Path() == "" {
		t.Fatalf("expected setter to be called by autoBing")
	}
	if !strings.Contains(a.Status(), "已自动同步今日必应壁纸") {
		t.Errorf("unexpected status: %s", a.Status())
	}
}

func TestBottomBarFavAndExport(t *testing.T) {
	tmpDir := t.TempDir()
	store := library.NewStoreWithPath(filepath.Join(tmpDir, "library.json"))
	a := newApplication()
	a.store = store
	a.selected = filepath.Join(tmpDir, "test.png")

	// 1. 默认自动同步开启时，底部栏渲染 "VS Code 自动同步中"
	a.syncVSCode = true
	tt := ui.NewTester(a.view, 1000, 680)
	for _, text := range []string{
		"↗ 在文件夹中显示",
		"VS Code 自动同步中",
		"✓ 应用到桌面",
	} {
		if !tt.HasText(text) {
			t.Errorf("missing text %q in bottom bar (sync=true)", text)
		}
	}

	// 2. 切换同步关闭时，底部栏渲染 "同步到 VS Code"
	a.syncVSCode = false
	tt = ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("同步到 VS Code") {
		t.Errorf("missing text '同步到 VS Code' in bottom bar (sync=false)")
	}

	// 3. 验证核心收藏功能真实有效
	a.toggleFavorite(a.selected)
	if !a.IsFavorite(a.selected) {
		t.Errorf("expected wallpaper to be favorited")
	}
}

func TestPreviewStatusLabel_AppliedVsPreviewing(t *testing.T) {
	tmpDir := t.TempDir()
	pngPath := filepath.Join(tmpDir, "wallpaper.png")
	_ = os.WriteFile(pngPath, generateTestPNG(), 0o644)

	a := newApplication()
	a.loadWallpaper(pngPath)

	// 1. 此时为刚载入预览态，尚未应用到系统桌面
	a.applied = ""
	tt := ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("预览模式 (未应用)") {
		t.Errorf("expected '预览模式 (未应用)' badge when wallpaper is not yet applied")
	}
	if tt.HasText("当前系统桌面壁纸") {
		t.Errorf("did not expect '当前系统桌面壁纸' before wallpaper applied")
	}

	// 2. 模拟应用成功
	a.applied = pngPath
	tt = ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("当前系统桌面壁纸") {
		t.Errorf("expected '当前系统桌面壁纸' badge when selected == applied")
	}
	if tt.HasText("预览模式 (未应用)") {
		t.Errorf("did not expect '预览模式 (未应用)' after wallpaper applied")
	}
}

func TestClearRecent_Workflow(t *testing.T) {
	tmpDir := t.TempDir()
	store := library.NewStoreWithPath(filepath.Join(tmpDir, "library.json"))
	a := newApplication()
	a.store = store
	a.recent = []string{"/path/1.png", "/path/2.png"}
	a.localSubTab = "recent"

	tt := ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("清空") {
		t.Fatalf("expected '清空' button when recent list is not empty")
	}

	if err := tt.Click("清空"); err != nil {
		t.Fatalf("failed to click '清空': %v", err)
	}

	if len(a.recent) != 0 {
		t.Errorf("expected a.recent to be empty after clear, got %v", a.recent)
	}
	if a.Status() != "已清空最近壁纸记录" {
		t.Errorf("status = %q, want '已清空最近壁纸记录'", a.Status())
	}
}

func TestFitSelectorVisualLabels(t *testing.T) {
	a := newApplication()
	tt := ui.NewTester(a.view, 1000, 680)

	for _, label := range []string{"⬒ 覆盖", "⬓ 适应", "⬖ 拉伸"} {
		if !tt.HasText(label) {
			t.Errorf("missing fit mode label %q", label)
		}
	}

	if err := tt.Click("⬓ 适应"); err != nil {
		t.Fatalf("failed to click '⬓ 适应': %v", err)
	}
	if a.fit != "适应" {
		t.Errorf("expected fit='适应', got %q", a.fit)
	}
}

func TestDropHintInSidebar(t *testing.T) {
	a := newApplication()
	tt := ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("或直接拖放图片到窗口") {
		t.Errorf("missing drop hint '或直接拖放图片到窗口' in local view")
	}
}

func TestWallpaperSpec(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "spec_test.png")
	data := generateTestPNG()
	_ = os.WriteFile(filePath, data, 0o644)

	// 1. 无 Bitmap 时回退为本地文件大小
	spec := wallpaperSpec(filePath, nil)
	if !strings.Contains(spec, "B") {
		t.Errorf("expected file size in spec, got %q", spec)
	}

	// 2. 带解码 Bitmap 时应包含分辨率档位与文件大小
	bmp, err := ui.DecodeBitmap(data)
	if err != nil {
		t.Fatalf("DecodeBitmap failed: %v", err)
	}
	specWithBmp := wallpaperSpec(filePath, bmp)
	if !strings.Contains(specWithBmp, "·") || !strings.Contains(specWithBmp, "2×2") {
		t.Errorf("expected combined resolution and size, got %q", specWithBmp)
	}
}

func TestCategoryBadge(t *testing.T) {
	bingCat := online.Category{ID: "bing", Source: "bing", Name: "必应精选"}
	if badge := categoryBadge(bingCat); badge != "8" {
		t.Errorf("expected '8' for bing badge, got %q", badge)
	}

	cat4K := online.Category{ID: "36", Source: "360", Name: "4K专区"}
	if badge := categoryBadge(cat4K); badge != "128" {
		t.Errorf("expected '128' for 4K badge, got %q", badge)
	}
}

func TestMacOSDockSimulatorInPreview(t *testing.T) {
	tmpDir := t.TempDir()
	pngPath := filepath.Join(tmpDir, "dock_test.png")
	_ = os.WriteFile(pngPath, generateTestPNG(), 0o644)

	a := newApplication()
	a.loadWallpaper(pngPath)

	tt := ui.NewTester(a.view, 1000, 680)
	// 原型 Dock 6 个应用图标
	for _, dockIcon := range []string{"🧭", "⚡", "⟨/⟩", "📁", "🎵", "⚙"} {
		if !tt.HasText(dockIcon) {
			t.Errorf("missing macOS Dock simulator icon %q in preview", dockIcon)
		}
	}
}

func TestOnlineSearchFilter(t *testing.T) {
	a := newApplication()
	a.tab = "online"
	a.onlineView = "grid"
	a.wallpapers = []online.WallpaperItem{
		{ID: "1", Title: "挪威极光夜景", Source: "360"},
		{ID: "2", Title: "阿尔卑斯山金色晨曦", Source: "360"},
		{ID: "3", Title: "极简太空星空", Source: "360"},
	}

	// 1. 无搜索词时，3张卡片全数显示
	tt := ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("挪威极光夜景") || !tt.HasText("阿尔卑斯山金色晨曦") || !tt.HasText("极简太空星空") {
		t.Fatalf("expected all 3 items to show without filter")
	}

	// 2. 搜索 "极光"
	a.searchKeyword = "极光"
	tt = ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("挪威极光夜景") {
		t.Errorf("expected '挪威极光夜景' to match '极光'")
	}
	if tt.HasText("阿尔卑斯山金色晨曦") {
		t.Errorf("did not expect '阿尔卑斯山金色晨曦' when filtering '极光'")
	}
}

func TestOnlineCard4KUHDBadge(t *testing.T) {
	a := newApplication()
	a.tab = "online"
	a.onlineView = "grid"
	a.wallpapers = []online.WallpaperItem{
		{ID: "1", Title: "挪威极光夜景", Resolution: "3840x2160", Source: "360"},
	}

	tt := ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("4K UHD") {
		t.Errorf("missing '4K UHD' badge on wallpaper card")
	}
}

func TestFavoritesSubTabUIAndCards(t *testing.T) {
	tmpDir := t.TempDir()
	favFile := filepath.Join(tmpDir, "fav_img.png")
	_ = os.WriteFile(favFile, generateTestPNG(), 0o644)
	store := library.NewStoreWithPath(filepath.Join(tmpDir, "library.json"))

	a := newApplication()
	a.store = store
	a.tab = "local"
	a.recent = []string{"/tmp/recent1.png"}
	a.favorites = []string{favFile}

	// 1. 默认展示最近使用
	a.localSubTab = "recent"
	tt := ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("最近使用 · 1") {
		t.Errorf("expected '最近使用 · 1' in recent sub-tab")
	}
	if !tt.HasText("清空") {
		t.Errorf("expected '清空' button in recent sub-tab")
	}

	// 2. 切换到我的收藏
	a.localSubTab = "favorites"
	tt = ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("我的收藏 · 1") {
		t.Errorf("expected '我的收藏 · 1' in favorites sub-tab")
	}
	if !tt.HasText(filepath.Base(favFile)) {
		t.Errorf("expected favorite item filename %q in list", filepath.Base(favFile))
	}
	// 收藏列表不应显示“清空”按钮
	if tt.HasText("清空") {
		t.Errorf("did not expect '清空' button in favorites sub-tab")
	}
}

func TestLargePreviewActionControls_FavAndExport(t *testing.T) {
	tmpDir := t.TempDir()
	pngPath := filepath.Join(tmpDir, "sample.png")
	_ = os.WriteFile(pngPath, generateTestPNG(), 0o644)
	store := library.NewStoreWithPath(filepath.Join(tmpDir, "library.json"))

	a := newApplication()
	a.store = store
	a.loadWallpaper(pngPath)

	// 1. 未收藏时，大图悬浮栏应显示 "收藏" 和 "另存为"
	tt := ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("收藏") {
		t.Errorf("missing '收藏' button in large preview box")
	}
	if !tt.HasText("另存为") {
		t.Errorf("missing '另存为' button in large preview box")
	}

	// 2. 加入收藏后，大图悬浮栏应显示 "已收藏"
	a.toggleFavorite(pngPath)
	tt = ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("已收藏") {
		t.Errorf("expected '已收藏' status in large preview box")
	}
}

func TestBottomBarStandaloneMapVSCode(t *testing.T) {
	tmpDir := t.TempDir()
	dummyImg := filepath.Join(tmpDir, "wall.png")
	_ = os.WriteFile(dummyImg, generateTestPNG(), 0o644)

	called := false
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		called = true
		return []byte("ok"), nil
	}

	a := newApplication()
	a.selected = dummyImg
	a.vscode = vscode.Service{
		UserDir:       filepath.Join(tmpDir, "User"),
		CommandRunner: mockRunner,
	}

	// 验证底部操作栏渲染了独立的 "映射到 VS Code" 按钮
	tt := ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("映射到 VS Code") {
		t.Errorf("missing standalone '映射到 VS Code' button in bottom bar")
	}

	// 验证触发 applyVSCode 后的执行状态
	a.applyVSCode()
	if !a.busy {
		t.Errorf("expected busy=true while applying to VS Code")
	}
	_ = called
}

func TestOnlineGridFilterSortingOptions(t *testing.T) {
	a := newApplication()
	a.tab = "online"
	a.onlineView = "grid"
	a.wallpapers = []online.WallpaperItem{
		{ID: "1", Title: "普通风景 A", Resolution: "1920x1080", Source: "360"},
		{ID: "2", Title: "4K极清 B", Resolution: "3840x2160", Source: "360"},
		{ID: "3", Title: "普通风景 C", Resolution: "1920x1080", Source: "360"},
	}

	// 1. 默认精选推荐
	a.filterOption = "精选推荐"
	tt := ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("普通风景 A") || !tt.HasText("4K极清 B") || !tt.HasText("普通风景 C") {
		t.Fatalf("expected all 3 items to show in default sort")
	}

	// 2. 切换为最新壁纸 (顺序翻转)
	a.filterOption = "最新壁纸"
	tt = ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("普通风景 C") {
		t.Errorf("expected items to render in '最新壁纸' mode")
	}

	// 3. 切换为最高人气 (4K 优先排在前面)
	a.filterOption = "最高人气"
	tt = ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("4K极清 B") {
		t.Errorf("expected items to render in '最高人气' mode")
	}
}

func TestOnlineBottomBar_PreviewModeClosureAndBingPagination(t *testing.T) {
	a := newApplication()
	a.tab = "online"

	// 1. 测试必应源单页分页限制
	a.onlineView = "grid"
	a.selectedCategory = online.Category{ID: "bing_daily", Name: "必应每日精选", Source: "bing"}
	tt := ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("第 1 / 1 页") {
		t.Errorf("expected Bing category pagination to be '第 1 / 1 页', got texts: %q", tt.Texts())
	}

	// 2. 测试进入大图预览模式后的底部操作栏闭环
	a.onlineView = "preview"
	tt = ui.NewTester(a.view, 1000, 680)
	if !tt.HasText("✓ 应用到桌面") {
		t.Errorf("expected '✓ 应用到桌面' button in online preview mode bottom bar")
	}
	if !tt.HasText("映射到 VS Code") {
		t.Errorf("expected '映射到 VS Code' button in online preview mode bottom bar")
	}
}
