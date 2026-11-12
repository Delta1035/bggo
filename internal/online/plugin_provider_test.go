package online

import (
	"context"
	"testing"

	"bggo/internal/plugin"
)

func TestPluginProviderMapsRealPluginResults(t *testing.T) {
	source, err := plugin.LoadSource(plugin.Manifest{
		Type:        "bggo-wallpaper",
		APIVersion:  1,
		ID:          "mapped",
		Name:        "Mapped",
		Version:     "1.0.0",
		Permissions: plugin.Permissions{Network: []string{"*.example"}},
	}, "index.js", `
export function categories() {
  return [{ id: "daily", name: "Daily", description: "From plugin", badge: "3" }];
}
export function list() {
  return { items: [{ id: "item-1", title: "One", thumbUrl: "https://img.example/thumb.jpg", fullUrl: "https://img.example/full.jpg", width: 1920, height: 1080 }] };
}
`, t.TempDir())
	if err != nil {
		t.Fatalf("LoadSource() failed: %v", err)
	}
	defer source.Close()

	provider := NewPluginProvider(source)
	categories, err := provider.Categories(context.Background())
	if err != nil {
		t.Fatalf("Categories() failed: %v", err)
	}
	if len(categories) != 1 || categories[0].Source != "mapped" || categories[0].Description != "From plugin" || categories[0].Badge != "3" {
		t.Fatalf("Categories() = %#v", categories)
	}

	page, err := provider.FetchPage(context.Background(), "daily", 1, 18)
	if err != nil {
		t.Fatalf("FetchPage() failed: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "mapped:item-1" || page.Items[0].Source != "mapped" || page.Items[0].Width != 1920 {
		t.Fatalf("FetchPage() = %#v", page)
	}
}
