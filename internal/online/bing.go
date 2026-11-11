package online

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type bingResponse struct {
	Images []struct {
		StartDate     string `json:"startdate"`
		FullStartDate string `json:"fullstartdate"`
		EndDate       string `json:"enddate"`
		URL           string `json:"url"`
		URLBase       string `json:"urlbase"`
		Copyright     string `json:"copyright"`
		Title         string `json:"title"`
		HSH           string `json:"hsh"`
	} `json:"images"`
}

// BingProvider 必应壁纸源
type BingProvider struct {
	BaseURL string
	Client  *http.Client
}

// NewBingProvider 创建必应提供者
func NewBingProvider() *BingProvider {
	return &BingProvider{
		BaseURL: "https://cn.bing.com",
		Client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (b *BingProvider) Name() string {
	return "bing"
}

func (b *BingProvider) Categories(ctx context.Context) ([]Category, error) {
	return []Category{
		{
			ID:     "bing_daily",
			Name:   "必应每日精选",
			Source: "bing",
		},
	}, nil
}

func (b *BingProvider) Fetch(ctx context.Context, categoryID string, page, pageSize int) ([]WallpaperItem, error) {
	baseURL := strings.TrimRight(b.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://cn.bing.com"
	}

	// Bing API: n 支持 1~8 张，idx 支持 0~7
	n := pageSize
	if n <= 0 || n > 8 {
		n = 8
	}
	idx := (page - 1) * n
	if idx < 0 {
		idx = 0
	}
	if idx > 7 {
		// Bing 官方接口最多只保留最近 7~8 天壁纸
		idx = 7
	}

	apiURL := fmt.Sprintf("%s/HPImageArchive.aspx?format=js&idx=%d&n=%d&mkt=zh-CN", baseURL, idx, n)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	client := b.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 Bing 接口失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Bing 接口返回异常状态码: %d", resp.StatusCode)
	}

	var data bingResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("解析 Bing 响应失败: %w", err)
	}

	var items []WallpaperItem
	for _, img := range data.Images {
		fullURL := img.URL
		if !strings.HasPrefix(fullURL, "http://") && !strings.HasPrefix(fullURL, "https://") {
			if !strings.HasPrefix(fullURL, "/") {
				fullURL = "/" + fullURL
			}
			fullURL = baseURL + fullURL
		}

		title := strings.TrimSpace(img.Title)
		if title == "" {
			// 若 title 为空，尝试用版权信息
			title = strings.TrimSpace(img.Copyright)
		}
		if title == "" {
			title = "Bing 每日壁纸"
		}

		thumbURL := fullURL
		if strings.Contains(thumbURL, "?") {
			thumbURL += "&pid=hp&w=384&h=216"
		} else {
			thumbURL += "?pid=hp&w=384&h=216"
		}

		id := img.HSH
		if id == "" {
			id = img.StartDate
		}
		if id == "" {
			id = fmt.Sprintf("bing_%d", time.Now().UnixNano())
		}

		items = append(items, WallpaperItem{
			ID:         id,
			Title:      title,
			ThumbURL:   thumbURL,
			FullURL:    fullURL,
			Resolution: "1920x1080",
			Source:     "bing",
			Copyright:  img.Copyright,
		})
	}

	return items, nil
}
