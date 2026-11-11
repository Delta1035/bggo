package online

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBingProviderCategories(t *testing.T) {
	bp := NewBingProvider()
	cats, err := bp.Categories(context.Background())
	if err != nil {
		t.Fatalf("Categories() failed: %v", err)
	}
	if len(cats) != 1 {
		t.Fatalf("expected 1 category, got %d", len(cats))
	}
	if cats[0].Source != "bing" || cats[0].ID != "bing_daily" {
		t.Errorf("unexpected category: %+v", cats[0])
	}
}

func TestBingProviderFetchSuccess(t *testing.T) {
	fakeJSON := `{
		"images": [
			{
				"startdate": "20261111",
				"fullstartdate": "202611110000",
				"enddate": "20261112",
				"url": "/th?id=OHR.TestImage_ZH-CN12345_1920x1080.jpg",
				"urlbase": "/th?id=OHR.TestImage_ZH-CN12345",
				"copyright": "大西洋森林 (© Test/Getty)",
				"title": "奇妙的森林",
				"hsh": "test_hash_1"
			},
			{
				"startdate": "20261110",
				"fullstartdate": "202611100000",
				"enddate": "20261111",
				"url": "https://cn.bing.com/th?id=OHR.Test2_ZH-CN54321_1920x1080.jpg",
				"urlbase": "/th?id=OHR.Test2_ZH-CN54321",
				"copyright": "长白山天池 (© Photographer)",
				"title": "",
				"hsh": ""
			}
		]
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "HPImageArchive.aspx") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fakeJSON))
	}))
	defer ts.Close()

	bp := &BingProvider{
		BaseURL: ts.URL,
		Client:  ts.Client(),
	}

	items, err := bp.Fetch(context.Background(), "bing_daily", 1, 8)
	if err != nil {
		t.Fatalf("Fetch() failed: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// 验证第一个条目：相对路径拼接、标题与缩略图参数
	item1 := items[0]
	if item1.ID != "test_hash_1" {
		t.Errorf("expected ID test_hash_1, got %s", item1.ID)
	}
	if item1.Title != "奇妙的森林" {
		t.Errorf("expected title '奇妙的森林', got %s", item1.Title)
	}
	if !strings.HasPrefix(item1.FullURL, ts.URL) {
		t.Errorf("expected FullURL to start with %s, got %s", ts.URL, item1.FullURL)
	}
	if !strings.Contains(item1.ThumbURL, "pid=hp&w=384&h=216") {
		t.Errorf("expected ThumbURL to contain thumbnail query, got %s", item1.ThumbURL)
	}

	// 验证第二个条目：绝对链接、Title为空时回退到 Copyright、hsh为空时使用 StartDate
	item2 := items[1]
	if item2.ID != "20261110" {
		t.Errorf("expected fallback ID 20261110, got %s", item2.ID)
	}
	if item2.Title != "长白山天池 (© Photographer)" {
		t.Errorf("expected title fallback to copyright, got %s", item2.Title)
	}
}

func TestBingProviderFetchErrors(t *testing.T) {
	// 1. 模拟 HTTP 500
	tsErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer tsErr.Close()

	bpErr := &BingProvider{
		BaseURL: tsErr.URL,
		Client:  tsErr.Client(),
	}

	if _, err := bpErr.Fetch(context.Background(), "bing_daily", 1, 8); err == nil {
		t.Errorf("expected error for 500 status, got nil")
	}

	// 2. 模拟畸形 JSON
	tsBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer tsBadJSON.Close()

	bpBadJSON := &BingProvider{
		BaseURL: tsBadJSON.URL,
		Client:  tsBadJSON.Client(),
	}

	if _, err := bpBadJSON.Fetch(context.Background(), "bing_daily", 1, 8); err == nil {
		t.Errorf("expected error for invalid JSON, got nil")
	}
}
