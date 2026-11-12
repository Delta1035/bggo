package main

import (
	"context"
	"testing"

	"bggo/internal/online"
)

func TestEmbeddedBingPluginUsesRuntimeAndRegistry(t *testing.T) {
	service, err := online.NewService(t.TempDir())
	if err != nil {
		t.Fatalf("NewService() failed: %v", err)
	}
	plugins := loadBuiltinWallpaperPluginsAt(service, t.TempDir())
	defer func() {
		for _, source := range plugins {
			source.Close()
		}
	}()

	provider, ok := service.Provider("bing")
	if !ok {
		t.Fatal("Bing provider was not registered")
	}
	pluginProvider, ok := provider.(*online.PluginProvider)
	if !ok {
		t.Fatalf("Bing provider type = %T, want plugin provider", provider)
	}
	categories, err := pluginProvider.Categories(context.Background())
	if err != nil {
		t.Fatalf("plugin Categories() failed: %v", err)
	}
	if len(categories) != 1 || categories[0].ID != "bing_daily" || categories[0].Description == "" {
		t.Fatalf("plugin Categories() = %#v", categories)
	}
}
