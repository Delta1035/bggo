package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bggo/internal/library"
	"bggo/internal/online"
	"bggo/internal/vscode"
	"github.com/egoist/mygo/ui"
)

func TestInitialView(t *testing.T) {
	a := &application{fit: "覆盖", status: "选择一张图片开始"}
	tt := ui.NewTester(a.view, 1000, 680)
	for _, text := range []string{
		"BGGo",
		AppVersion,
		"桌面壁纸",
		"还没有选择壁纸",
		"应用到桌面",
		"映射到 VS Code",
		"同步到 VS Code",
		"检查更新",
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

func TestApplyVSCode_AutoInstallExtension(t *testing.T) {
	tmp := t.TempDir()
	userDir := filepath.Join(tmp, "User")
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
		t.Fatal("timeout waiting for auto-install of extension")
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
		"💻 本地壁纸",
		"🌐 在线图库",
		"壁纸分类",
		"必应每日精选",
		"4K专区",
		"在线壁纸 · 必应每日精选",
		"数据源: 微软必应官方",
		"⊞ 壁纸列表",
		"🖼 高清预览",
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
		"3840x2160",
		"极简星空大片",
		"1920x1080",
		"👁 预览",
		"应用",
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
	calledPath string
}

func (m *testWallpaperSetter) Set(path string) error {
	m.calledPath = path
	return nil
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

	mockSetter := &testWallpaperSetter{}
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
	for a.busy && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if mockSetter.calledPath == "" {
		t.Fatalf("expected setter to be called with downloaded path")
	}
	if a.selected != mockSetter.calledPath {
		t.Errorf("a.selected %q != setter path %q", a.selected, mockSetter.calledPath)
	}
	if !strings.Contains(a.status, "壁纸已成功应用到桌面") {
		t.Errorf("expected success status, got %q", a.status)
	}
	if a.preview == nil {
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
	for a.busy && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if a.onlineView != "preview" {
		t.Errorf("expected onlineView = preview, got %s", a.onlineView)
	}
	if a.preview == nil {
		t.Fatalf("expected non-nil preview bitmap")
	}
	if a.selected == "" {
		t.Errorf("expected non-empty selected path")
	}
	if !strings.Contains(a.status, "已加载高清预览") {
		t.Errorf("expected preview status, got %q", a.status)
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
	for (a.loadingOnline || len(a.categories) == 0 || len(a.wallpapers) == 0) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if len(a.categories) != 2 {
		t.Fatalf("expected 2 categories, got %d (%+v)", len(a.categories), a.categories)
	}
	if len(a.wallpapers) != 1 {
		t.Fatalf("expected 1 wallpaper, got %d (%+v)", len(a.wallpapers), a.wallpapers)
	}
	if a.wallpapers[0].Title != "Bing今日大图" {
		t.Errorf("expected wallpaper title 'Bing今日大图', got %q", a.wallpapers[0].Title)
	}
}

