package online

// WallpaperItem 代表一张在线壁纸的元数据
type WallpaperItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Author      string `json:"author,omitempty"`
	ThumbURL    string `json:"thumb_url"`
	FullURL     string `json:"full_url"`
	Resolution  string `json:"resolution,omitempty"`
	Source      string `json:"source"` // "bing" | "360"
	Copyright   string `json:"copyright,omitempty"`
}

// Category 代表在线壁纸的分类
type Category struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"` // "bing" | "360"
}
