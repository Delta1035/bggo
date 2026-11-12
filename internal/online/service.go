package online

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

// Service 整合所有在线壁纸源与下载缓存的服务
type Service struct {
	Bing     *BingProvider
	Qihoo    *QihooProvider
	Cache    *CacheManager
	Client   *http.Client
	Registry *SourceRegistry

	categoriesMu sync.RWMutex
	categories   []Category
}

// NewService 创建在线壁纸整合服务
func NewService(cacheDir string) (*Service, error) {
	cache, err := NewCacheManager(cacheDir)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	bing := NewBingProvider()
	bing.Client = client
	qihoo := NewQihooProvider()
	qihoo.Client = client

	registry := NewSourceRegistry()
	if err := registry.Register(bing); err != nil {
		return nil, err
	}
	if err := registry.Register(qihoo); err != nil {
		return nil, err
	}

	return &Service{
		Bing:     bing,
		Qihoo:    qihoo,
		Cache:    cache,
		Client:   client,
		Registry: registry,
	}, nil
}

// RegisterProvider adds a built-in or plugin-backed source and invalidates the
// aggregated category cache so the next read sees it.
func (s *Service) RegisterProvider(provider Provider) error {
	if s.Registry == nil {
		s.Registry = NewSourceRegistry()
	}
	if err := s.Registry.Register(provider); err != nil {
		return err
	}
	s.categoriesMu.Lock()
	s.categories = nil
	s.categoriesMu.Unlock()
	return nil
}

// ReplaceProvider installs a provider with the same ID over the current
// implementation, retaining the original source ordering.
func (s *Service) ReplaceProvider(provider Provider) error {
	if s.Registry == nil {
		s.Registry = NewSourceRegistry()
	}
	if err := s.Registry.Replace(provider); err != nil {
		return err
	}
	s.categoriesMu.Lock()
	s.categories = nil
	s.categoriesMu.Unlock()
	return nil
}

// Provider returns the source registered under name.
func (s *Service) Provider(name string) (Provider, bool) {
	return s.providersByID().Get(name)
}

// RemoveProvider unregisters a source and invalidates the category cache.
func (s *Service) RemoveProvider(name string) bool {
	if s.Registry == nil || !s.Registry.Remove(name) {
		return false
	}
	s.categoriesMu.Lock()
	s.categories = nil
	s.categoriesMu.Unlock()
	return true
}

// GetCategories 获取合并后的所有可用分类
func (s *Service) GetCategories(ctx context.Context) ([]Category, error) {
	s.categoriesMu.RLock()
	if len(s.categories) > 0 {
		cats := make([]Category, len(s.categories))
		copy(cats, s.categories)
		s.categoriesMu.RUnlock()
		return cats, nil
	}
	s.categoriesMu.RUnlock()

	var result []Category
	var lastErr error
	for _, provider := range s.providers() {
		categories, err := provider.Categories(ctx)
		if err != nil {
			lastErr = err
			continue
		}
		result = append(result, categories...)
	}
	if len(result) == 0 {
		if lastErr == nil {
			lastErr = fmt.Errorf("没有注册壁纸源")
		}
		return nil, fmt.Errorf("无法加载任何壁纸分类: %w", lastErr)
	}

	s.categoriesMu.Lock()
	s.categories = result
	s.categoriesMu.Unlock()

	return result, nil
}

// GetWallpapers 根据分类拉取壁纸列表
func (s *Service) GetWallpapers(ctx context.Context, category Category, page, pageSize int) ([]WallpaperItem, error) {
	result, err := s.GetWallpaperPage(ctx, category, page, pageSize)
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

// GetWallpaperPage returns items and source-provided pagination metadata.
func (s *Service) GetWallpaperPage(ctx context.Context, category Category, page, pageSize int) (WallpaperPage, error) {
	if provider, ok := s.providersByID().Get(category.Source); ok {
		if paged, ok := provider.(PageProvider); ok {
			return paged.FetchPage(ctx, category.ID, page, pageSize)
		}
		items, err := provider.Fetch(ctx, category.ID, page, pageSize)
		return WallpaperPage{Items: items, More: len(items) >= pageSize}, err
	}
	return WallpaperPage{}, fmt.Errorf("未找到壁纸源: %s", category.Source)
}

func (s *Service) providers() []Provider {
	if s.Registry != nil {
		return s.Registry.Sources()
	}
	var providers []Provider
	if s.Bing != nil {
		providers = append(providers, s.Bing)
	}
	if s.Qihoo != nil {
		providers = append(providers, s.Qihoo)
	}
	return providers
}

func (s *Service) providersByID() *SourceRegistry {
	if s.Registry != nil {
		return s.Registry
	}
	registry := NewSourceRegistry()
	for _, provider := range s.providers() {
		_ = registry.Register(provider)
	}
	return registry
}

// DownloadWallpaper 下载高清原图并持久化保存到本地缓存
func (s *Service) DownloadWallpaper(ctx context.Context, item WallpaperItem) (string, error) {
	if item.FullURL == "" {
		return "", fmt.Errorf("壁纸链接为空")
	}

	ext := path.Ext(strings.Split(item.FullURL, "?")[0])
	if ext == "" {
		ext = ".jpg"
	}

	key := s.Cache.KeyForURL(item.FullURL)
	if localPath, ok := s.Cache.Get(key, ext); ok {
		return localPath, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.FullURL, nil)
	if err != nil {
		return "", fmt.Errorf("创建下载请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "BGGo-Wallpaper/1.0")

	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载壁纸图片失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载返回异常状态码: %d", resp.StatusCode)
	}

	localPath, err := s.Cache.Save(key, ext, resp.Body)
	if err != nil {
		return "", fmt.Errorf("写入本地缓存失败: %w", err)
	}

	return localPath, nil
}

// FetchThumbnailBytes 拉取缩略图二进制流用于内存解码
func (s *Service) FetchThumbnailBytes(ctx context.Context, thumbURL string) ([]byte, error) {
	if thumbURL == "" {
		return nil, fmt.Errorf("缩略图链接为空")
	}

	ext := path.Ext(strings.Split(thumbURL, "?")[0])
	if ext == "" {
		ext = ".jpg"
	}
	key := "thumb_" + s.Cache.KeyForURL(thumbURL)
	if localPath, ok := s.Cache.Get(key, ext); ok {
		return os.ReadFile(localPath)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, thumbURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建缩略图请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "BGGo-Wallpaper/1.0")

	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取缩略图失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("缩略图接口返回异常状态码: %d", resp.StatusCode)
	}

	// 写入本地缓存以备下次秒开
	localPath, err := s.Cache.Save(key, ext, resp.Body)
	if err == nil {
		return os.ReadFile(localPath)
	}

	// 缓存失败时直接从流读取
	return io.ReadAll(resp.Body)
}
