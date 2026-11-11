package online

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQihooProviderCategories(t *testing.T) {
	fakeCategoriesJSON := `{
		"errno": "0",
		"errmsg": "正常",
		"total": "2",
		"data": [
			{"id": "36", "name": "4K专区"},
			{"id": "9", "name": "风景大片"}
		]
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fakeCategoriesJSON))
	}))
	defer ts.Close()

	qp := &QihooProvider{
		CatURL: ts.URL,
		Client: ts.Client(),
	}

	cats, err := qp.Categories(context.Background())
	if err != nil {
		t.Fatalf("Categories() failed: %v", err)
	}

	if len(cats) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(cats))
	}
	if cats[0].ID != "36" || cats[0].Name != "4K专区" || cats[0].Source != "360" {
		t.Errorf("unexpected category 0: %+v", cats[0])
	}
	if cats[1].ID != "9" || cats[1].Name != "风景大片" || cats[1].Source != "360" {
		t.Errorf("unexpected category 1: %+v", cats[1])
	}
}

func TestQihooProviderFetch(t *testing.T) {
	fakeWallpapersJSON := `{
		"errno": "0",
		"errmsg": "正常",
		"total": "1",
		"data": [
			{
				"id": "1001",
				"class_id": "9",
				"resolution": "1920x1080",
				"url": "http://p2.qhimg.com/bdr/__85/t019ca75cd449be50c1.jpg",
				"url_thumb": "http://p2.qhimg.com/bdr/__85/t019ca75cd449be50c1.jpg",
				"tag": "_全部_ _风景_",
				"utag": "日落海岸 晚霞海浪 沙滩"
			}
		]
	}`

	var lastReqURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastReqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fakeWallpapersJSON))
	}))
	defer ts.Close()

	qp := &QihooProvider{
		BaseURL: ts.URL,
		Client:  ts.Client(),
	}

	// page=2, pageSize=15 => start=15
	items, err := qp.Fetch(context.Background(), "9", 2, 15)
	if err != nil {
		t.Fatalf("Fetch() failed: %v", err)
	}

	if !strings.Contains(lastReqURL, "start=15") || !strings.Contains(lastReqURL, "count=15") || !strings.Contains(lastReqURL, "cid=9") {
		t.Errorf("unexpected request URL: %s", lastReqURL)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	item := items[0]
	if item.ID != "360_1001" {
		t.Errorf("expected ID 360_1001, got %s", item.ID)
	}
	if item.Title != "日落海岸 · 晚霞海浪" {
		t.Errorf("expected title '日落海岸 · 晚霞海浪', got %q", item.Title)
	}
	// 验证协议升级为 https，且高清图转为 1920_1080_80
	if !strings.Contains(item.FullURL, "https://") || !strings.Contains(item.FullURL, "/bdm/1920_1080_80/") {
		t.Errorf("unexpected FullURL: %s", item.FullURL)
	}
	// 验证缩略图转换为 480_270_80
	if !strings.Contains(item.ThumbURL, "/bdm/480_270_80/") {
		t.Errorf("unexpected ThumbURL: %s", item.ThumbURL)
	}
}

func TestQihooProviderErrors(t *testing.T) {
	tsErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer tsErr.Close()

	qp := &QihooProvider{
		BaseURL: tsErr.URL,
		CatURL:  tsErr.URL,
		Client:  tsErr.Client(),
	}

	if _, err := qp.Categories(context.Background()); err == nil {
		t.Errorf("expected error for Categories on 502, got nil")
	}
	if _, err := qp.Fetch(context.Background(), "9", 1, 10); err == nil {
		t.Errorf("expected error for Fetch on 502, got nil")
	}
}
