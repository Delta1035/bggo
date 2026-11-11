package online

import "context"

// Provider 在线壁纸源接口
type Provider interface {
	Name() string
	Categories(ctx context.Context) ([]Category, error)
	Fetch(ctx context.Context, categoryID string, page, pageSize int) ([]WallpaperItem, error)
}
