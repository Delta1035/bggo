package online

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestServiceIntegration(t *testing.T) {
	tmpCache := t.TempDir()

	var downloadHits int32
	var thumbHits int32

	fakeBingJSON := `{
		"images": [
			{
				"startdate": "20261111",
				"url": "/bing_full.jpg",
				"copyright": "Bing Image",
				"title": "Bing Wallpaper",
				"hsh": "bing_1"
			}
		]
	}`

	fakeQihooCats := `{
		"errno": "0",
		"data": [{"id": "9", "name": "风景"}]
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/HPImageArchive.aspx":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fakeBingJSON))
		case "/index.php":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fakeQihooCats))
		case "/bing_full.jpg":
			atomic.AddInt32(&downloadHits, 1)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("image-binary-stream-123456"))
		case "/thumb.jpg":
			atomic.AddInt32(&thumbHits, 1)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("thumb-bytes-999"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	svc, err := NewService(tmpCache)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// 注入 mock 客户端与地址
	svc.Client = ts.Client()
	svc.Bing.BaseURL = ts.URL
	svc.Bing.Client = ts.Client()
	svc.Qihoo.CatURL = ts.URL
	svc.Qihoo.BaseURL = ts.URL
	svc.Qihoo.Client = ts.Client()

	// 1. 测试合并分类
	cats, err := svc.GetCategories(context.Background())
	if err != nil {
		t.Fatalf("GetCategories failed: %v", err)
	}
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories (bing + qihoo), got %d", len(cats))
	}
	if cats[0].Source != "bing" || cats[1].Source != "360" {
		t.Errorf("unexpected categories: %+v", cats)
	}

	// 再次调用应当命中内存缓存
	cats2, _ := svc.GetCategories(context.Background())
	if len(cats2) != 2 {
		t.Errorf("expected 2 categories from memory cache")
	}

	// 2. 测试拉取壁纸
	items, err := svc.GetWallpapers(context.Background(), cats[0], 1, 8)
	if err != nil {
		t.Fatalf("GetWallpapers failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item := items[0]

	// 3. 测试原图下载与本地持久化
	path1, err := svc.DownloadWallpaper(context.Background(), item)
	if err != nil {
		t.Fatalf("DownloadWallpaper failed: %v", err)
	}
	if _, err := os.Stat(path1); err != nil {
		t.Fatalf("downloaded file does not exist: %v", err)
	}
	if atomic.LoadInt32(&downloadHits) != 1 {
		t.Errorf("expected 1 download hit, got %d", atomic.LoadInt32(&downloadHits))
	}

	// 第二次下载同一张壁纸应命中本地缓存，不再触发网络请求
	path2, err := svc.DownloadWallpaper(context.Background(), item)
	if err != nil {
		t.Fatalf("second DownloadWallpaper failed: %v", err)
	}
	if path1 != path2 {
		t.Errorf("expected same path on cache hit, got %s != %s", path1, path2)
	}
	if atomic.LoadInt32(&downloadHits) != 1 {
		t.Errorf("expected still 1 download hit on cache hit, got %d", atomic.LoadInt32(&downloadHits))
	}

	// 4. 测试缩略图拉取与缓存
	thumbURL := ts.URL + "/thumb.jpg"
	data1, err := svc.FetchThumbnailBytes(context.Background(), thumbURL)
	if err != nil {
		t.Fatalf("FetchThumbnailBytes failed: %v", err)
	}
	if string(data1) != "thumb-bytes-999" {
		t.Errorf("unexpected thumb data: %s", string(data1))
	}
	if atomic.LoadInt32(&thumbHits) != 1 {
		t.Errorf("expected 1 thumb hit, got %d", atomic.LoadInt32(&thumbHits))
	}

	// 第二次拉取同一缩略图应命中本地缓存
	data2, err := svc.FetchThumbnailBytes(context.Background(), thumbURL)
	if err != nil {
		t.Fatalf("second FetchThumbnailBytes failed: %v", err)
	}
	if string(data2) != "thumb-bytes-999" {
		t.Errorf("unexpected second thumb data: %s", string(data2))
	}
	if atomic.LoadInt32(&thumbHits) != 1 {
		t.Errorf("expected still 1 thumb hit on cache hit, got %d", atomic.LoadInt32(&thumbHits))
	}
}

func TestSourceRegistryRoutesProviders(t *testing.T) {
	registry := NewSourceRegistry()
	first := NewBingProvider()
	second := NewQihooProvider()

	if err := registry.Register(first); err != nil {
		t.Fatalf("Register(first) failed: %v", err)
	}
	if err := registry.Register(second); err != nil {
		t.Fatalf("Register(second) failed: %v", err)
	}
	if got, ok := registry.Get("bing"); !ok || got != first {
		t.Fatalf("Get(bing) = %v, %v; want first provider", got, ok)
	}
	if got := registry.Sources(); len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("Sources() = %#v; want registration order", got)
	}
	if err := registry.Register(NewBingProvider()); !errors.Is(err, ErrDuplicateProvider) {
		t.Fatalf("duplicate Register() error = %v, want ErrDuplicateProvider", err)
	}
}
