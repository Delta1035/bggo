package online

import (
	"context"
	"fmt"

	"bggo/internal/plugin"
)

// PluginProvider adapts a sandboxed wallpaper plugin to the online.Provider
// interface used by the rest of the application.
type PluginProvider struct {
	plugin *plugin.Plugin
}

// NewPluginProvider creates a provider backed by a loaded wallpaper plugin.
func NewPluginProvider(source *plugin.Plugin) *PluginProvider {
	return &PluginProvider{plugin: source}
}

func (p *PluginProvider) Name() string { return p.plugin.ID() }

func (p *PluginProvider) Categories(ctx context.Context) ([]Category, error) {
	var categories []plugin.Category
	if err := p.plugin.Call(ctx, "categories", map[string]any{}, &categories); err != nil {
		return nil, fmt.Errorf("插件 %s 加载分类失败: %w", p.Name(), err)
	}
	result := make([]Category, 0, len(categories))
	for _, category := range categories {
		if category.ID == "" || category.Name == "" {
			continue
		}
		result = append(result, Category{
			ID:          category.ID,
			Name:        category.Name,
			Source:      p.Name(),
			Description: category.Description,
			Badge:       category.Badge,
		})
	}
	return result, nil
}

func (p *PluginProvider) Fetch(ctx context.Context, categoryID string, page, pageSize int) ([]WallpaperItem, error) {
	result, err := p.FetchPage(ctx, categoryID, page, pageSize)
	return result.Items, err
}

func (p *PluginProvider) FetchPage(ctx context.Context, categoryID string, page, pageSize int) (WallpaperPage, error) {
	var result plugin.ListResult
	params := map[string]any{
		"categoryId": categoryID,
		"page":       page,
		"pageSize":   pageSize,
	}
	if err := p.plugin.Call(ctx, "list", params, &result); err != nil {
		return WallpaperPage{}, fmt.Errorf("插件 %s 加载壁纸失败: %w", p.Name(), err)
	}
	return WallpaperPage{Items: p.items(result.Items), More: result.More, Total: result.Total}, nil
}

// Daily invokes the optional daily capability.
func (p *PluginProvider) Daily(ctx context.Context) ([]WallpaperItem, error) {
	var result plugin.ListResult
	if err := p.plugin.Call(ctx, "daily", map[string]any{}, &result); err != nil {
		return nil, fmt.Errorf("插件 %s 加载每日壁纸失败: %w", p.Name(), err)
	}
	return p.items(result.Items), nil
}

// Random invokes the optional random capability.
func (p *PluginProvider) Random(ctx context.Context, categoryID string, pageSize int) ([]WallpaperItem, error) {
	var result plugin.ListResult
	params := map[string]any{"categoryId": categoryID, "pageSize": pageSize}
	if err := p.plugin.Call(ctx, "random", params, &result); err != nil {
		return nil, fmt.Errorf("插件 %s 加载随机壁纸失败: %w", p.Name(), err)
	}
	return p.items(result.Items), nil
}

func (p *PluginProvider) items(source []plugin.WallpaperItem) []WallpaperItem {
	items := make([]WallpaperItem, 0, len(source))
	for _, item := range source {
		if item.ID == "" || item.FullURL == "" || !p.plugin.AllowsURL(item.FullURL) {
			continue
		}
		thumbURL := item.ThumbURL
		if thumbURL != "" && !p.plugin.AllowsURL(thumbURL) {
			thumbURL = ""
		}
		items = append(items, WallpaperItem{
			ID:         p.Name() + ":" + item.ID,
			Title:      item.Title,
			Author:     item.Author,
			ThumbURL:   thumbURL,
			FullURL:    item.FullURL,
			Width:      item.Width,
			Height:     item.Height,
			Resolution: item.Resolution,
			Source:     p.Name(),
			Copyright:  item.Copyright,
		})
	}
	return items
}
