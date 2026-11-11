package online

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type qihooCategoryResp struct {
	Errno  string `json:"errno"`
	Errmsg string `json:"errmsg"`
	Data   []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"data"`
}

type qihooWallpaperResp struct {
	Errno  string `json:"errno"`
	Errmsg string `json:"errmsg"`
	Total  string `json:"total"`
	Data   []struct {
		ID         string `json:"id"`
		ClassID    string `json:"class_id"`
		Resolution string `json:"resolution"`
		URL        string `json:"url"`
		URLThumb   string `json:"url_thumb"`
		Tag        string `json:"tag"`
		Utag       string `json:"utag"`
	} `json:"data"`
}

// QihooProvider 360壁纸提供者
type QihooProvider struct {
	BaseURL string // 默认为 http://wallpaper.apc.360.cn
	CatURL  string // 默认为 http://cdn.apc.360.cn
	Client  *http.Client
}

// NewQihooProvider 创建 360 壁纸源
func NewQihooProvider() *QihooProvider {
	return &QihooProvider{
		BaseURL: "http://wallpaper.apc.360.cn",
		CatURL:  "http://cdn.apc.360.cn",
		Client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (q *QihooProvider) Name() string {
	return "360"
}

func (q *QihooProvider) Categories(ctx context.Context) ([]Category, error) {
	catURL := strings.TrimRight(q.CatURL, "/")
	if catURL == "" {
		catURL = "http://cdn.apc.360.cn"
	}
	apiURL := fmt.Sprintf("%s/index.php?c=WallPaper&a=getAllCategoriesV2&from=360chrome", catURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	client := q.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 360 分类失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("360 分类接口返回状态码: %d", resp.StatusCode)
	}

	var res qihooCategoryResp
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("解析 360 分类失败: %w", err)
	}

	var categories []Category
	for _, item := range res.Data {
		categories = append(categories, Category{
			ID:     item.ID,
			Name:   item.Name,
			Source: "360",
		})
	}
	return categories, nil
}

func (q *QihooProvider) Fetch(ctx context.Context, categoryID string, page, pageSize int) ([]WallpaperItem, error) {
	baseURL := strings.TrimRight(q.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://wallpaper.apc.360.cn"
	}

	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	start := (page - 1) * pageSize

	apiURL := fmt.Sprintf("%s/index.php?c=WallPaper&a=getAppsByCategory&cid=%s&start=%d&count=%d&from=360chrome",
		baseURL, categoryID, start, pageSize)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	client := q.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 360 壁纸列表失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("360 壁纸接口返回状态码: %d", resp.StatusCode)
	}

	var res qihooWallpaperResp
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("解析 360 壁纸列表失败: %w", err)
	}

	var items []WallpaperItem
	for _, wp := range res.Data {
		rawURL := wp.URL
		if rawURL == "" {
			rawURL = wp.URLThumb
		}
		if rawURL == "" {
			continue
		}

		// 提升协议到 https
		rawURL = strings.Replace(rawURL, "http://", "https://", 1)

		// 缩略图：使用 480x270 规格缩略图
		thumbURL := rawURL
		if strings.Contains(thumbURL, "/bdr/__85/") {
			thumbURL = strings.Replace(thumbURL, "/bdr/__85/", "/bdm/480_270_80/", 1)
		}

		// 高清原图：使用 1920x1080 高清规格
		fullURL := rawURL
		if strings.Contains(fullURL, "/bdr/__85/") {
			fullURL = strings.Replace(fullURL, "/bdr/__85/", "/bdm/1920_1080_80/", 1)
		}

		// 提取标题：优先取 utag 中的前两个词
		title := ""
		tags := strings.Fields(wp.Utag)
		if len(tags) > 0 {
			if len(tags) >= 2 {
				title = tags[0] + " · " + tags[1]
			} else {
				title = tags[0]
			}
		}
		if title == "" {
			title = "360壁纸 #" + wp.ID
		}

		resText := wp.Resolution
		if resText == "" {
			resText = "1920x1080"
		}

		items = append(items, WallpaperItem{
			ID:         "360_" + wp.ID,
			Title:      title,
			ThumbURL:   thumbURL,
			FullURL:    fullURL,
			Resolution: resText,
			Source:     "360",
			Copyright:  "360壁纸库",
		})
	}

	return items, nil
}
