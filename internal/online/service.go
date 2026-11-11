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
	Bing   *BingProvider
	Qihoo  *QihooProvider
	Cache  *CacheManager
	Client *http.Client

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

	return &Service{
		Bing:   bing,
		Qihoo:  qihoo,
		Cache:  cache,
		Client: client,
	}, nil
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

	// 1. Bing 每日壁纸
	bingCats, err := s.Bing.Categories(ctx)
	if err == nil {
		result = append(result, bingCats...)
	}

	// 2. 360 分类壁纸
	qihooCats, err := s.Qihoo.Categories(ctx)
	if err == nil {
		result = append(result, qihooCats...)
	} else if len(result) == 0 {
		return nil, fmt.Errorf("无法加载任何壁纸分类: %w", err)
	}

	s.categoriesMu.Lock()
	s.categories = result
	s.categoriesMu.Unlock()

	return result, nil
}

// GetWallpapers 根据分类拉取壁纸列表
func (s *Service) GetWallpapers(ctx context.Context, category Category, page, pageSize int) ([]WallpaperItem, error) {
	switch category.Source {
	case "bing":
		return s.Bing.Fetch(ctx, category.ID, page, pageSize)
	case "360":
		return s.Qihoo.Fetch(ctx, category.ID, page, pageSize)
	default:
		// 默认回退到 Bing
		return s.Bing.Fetch(ctx, category.ID, page, pageSize)
	}
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
