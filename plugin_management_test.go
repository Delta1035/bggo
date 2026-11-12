package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"bggo/internal/online"
	"bggo/internal/plugin"
)

func TestInstalledPluginLifecycle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	pluginDir := filepath.Join(root, "managed-demo")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(plugin.Manifest{
		Type:       "bggo-wallpaper",
		APIVersion: 1,
		ID:         "managed-demo",
		Name:       "Managed Demo",
		Version:    "1.0.0",
		Main:       "index.js",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte(`export function categories() { return [{ id: "demo", name: "Demo" }]; }`), 0o600); err != nil {
		t.Fatal(err)
	}

	service, err := online.NewService(t.TempDir())
	if err != nil {
		t.Fatalf("NewService() failed: %v", err)
	}
	a := &application{
		onlineSvc:       service,
		pluginRecords:   make(map[string]*managedPlugin),
		pluginRoot:      root,
		pluginDataRoot:  t.TempDir(),
		pluginState:     plugin.EnabledState{"managed-demo": true},
		pluginStatePath: filepath.Join(t.TempDir(), "plugin-state.json"),
	}
	a.loadInstalledPluginRecords()
	if _, ok := service.Provider("managed-demo"); !ok {
		t.Fatal("installed plugin was not registered")
	}
	if err := a.setPluginEnabled("managed-demo", false); err != nil {
		t.Fatalf("disable plugin failed: %v", err)
	}
	if _, ok := service.Provider("managed-demo"); ok {
		t.Fatal("disabled plugin is still registered")
	}
	if err := a.setPluginEnabled("managed-demo", true); err != nil {
		t.Fatalf("enable plugin failed: %v", err)
	}
	if _, ok := service.Provider("managed-demo"); !ok {
		t.Fatal("enabled plugin was not registered")
	}
	a.uninstallPlugin("managed-demo")
	if _, ok := a.pluginRecords["managed-demo"]; ok {
		t.Fatal("uninstalled plugin remains in catalog")
	}
	if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
		t.Fatalf("plugin directory still exists, stat error = %v", err)
	}
}

func TestPermissionExpansionIsDetected(t *testing.T) {
	a := &application{
		pluginRecords: map[string]*managedPlugin{
			"demo": {
				Manifest: plugin.Manifest{
					ID:          "demo",
					Permissions: plugin.Permissions{Network: []string{"api.example.com"}},
				},
			},
		},
	}
	if !a.permissionExpanded(plugin.Manifest{
		ID:          "demo",
		Permissions: plugin.Permissions{Network: []string{"api.example.com", "images.example.com"}},
	}) {
		t.Fatal("new network host was not detected")
	}
	if a.permissionExpanded(plugin.Manifest{
		ID:          "demo",
		Permissions: plugin.Permissions{Network: []string{"api.example.com"}},
	}) {
		t.Fatal("unchanged network permission was reported as expanded")
	}
}

func TestPluginManagementRollbackRestartsProvider(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	dataRoot := filepath.Join(t.TempDir(), "plugin-data")
	firstArchive := filepath.Join(t.TempDir(), "first.zip")
	secondArchive := filepath.Join(t.TempDir(), "second.zip")
	writeManagedPluginArchive(t, firstArchive, "managed-rollback", "1.0.0", "first")
	writeManagedPluginArchive(t, secondArchive, "managed-rollback", "2.0.0", "second")
	firstManifest := plugin.Manifest{
		Type:        "bggo-wallpaper",
		APIVersion:  1,
		ID:          "managed-rollback",
		Name:        "Managed Rollback",
		Version:     "1.0.0",
		Main:        "index.js",
		Permissions: plugin.Permissions{Network: []string{"example.com"}},
	}

	if _, err := plugin.InstallArchive(firstArchive, root); err != nil {
		t.Fatalf("install first archive failed: %v", err)
	}
	service, err := online.NewService(t.TempDir())
	if err != nil {
		t.Fatalf("NewService() failed: %v", err)
	}
	a := &application{
		onlineSvc:       service,
		pluginRecords:   make(map[string]*managedPlugin),
		pluginRoot:      root,
		pluginDataRoot:  dataRoot,
		pluginState:     plugin.EnabledState{},
		pluginStatePath: filepath.Join(t.TempDir(), "plugin-state.json"),
		pluginMetaPath:  filepath.Join(t.TempDir(), "plugin-metadata.json"),
		pluginMetadata:  plugin.MetadataState{},
	}
	t.Cleanup(func() {
		for _, source := range a.plugins {
			source.Close()
		}
	})
	if err := a.installPluginRecord(firstManifest); err != nil {
		t.Fatalf("register first plugin failed: %v", err)
	}
	if _, err := plugin.InstallArchive(secondArchive, root); err != nil {
		t.Fatalf("install second archive failed: %v", err)
	}
	if err := a.installPluginRecord(plugin.Manifest{
		Type:        "bggo-wallpaper",
		APIVersion:  1,
		ID:          "managed-rollback",
		Name:        "Managed Rollback",
		Version:     "2.0.0",
		Main:        "index.js",
		Permissions: plugin.Permissions{Network: []string{"example.com"}},
	}); err != nil {
		t.Fatalf("register second plugin failed: %v", err)
	}

	record := a.pluginRecords["managed-rollback"]
	if len(record.History) != 1 || record.History[0].Manifest.Version != "1.0.0" {
		t.Fatalf("history after update = %#v", record.History)
	}
	if got := managedPluginTitle(t, service, "second"); got != "second" {
		t.Fatalf("active plugin title = %q, want second", got)
	}
	versionID := record.History[0].ID
	if err := a.rollbackPlugin(record.Manifest.ID, versionID); err != nil {
		t.Fatalf("rollbackPlugin() failed: %v", err)
	}
	if record.Manifest.Version != "1.0.0" {
		t.Fatalf("record version after rollback = %q", record.Manifest.Version)
	}
	if len(record.History) != 1 || record.History[0].Manifest.Version != "2.0.0" {
		t.Fatalf("history after rollback = %#v", record.History)
	}
	if got := managedPluginTitle(t, service, "first"); got != "first" {
		t.Fatalf("rolled back plugin title = %q, want first", got)
	}
}

func managedPluginTitle(t *testing.T, service *online.Service, want string) string {
	t.Helper()
	page, err := service.GetWallpaperPage(context.Background(), online.Category{ID: "demo", Source: "managed-rollback"}, 1, 1)
	if err != nil {
		t.Fatalf("GetWallpaperPage() failed: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("GetWallpaperPage() returned %d items", len(page.Items))
	}
	return page.Items[0].Title
}

func writeManagedPluginArchive(t *testing.T, path, id, version, title string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	manifest, err := json.Marshal(plugin.Manifest{
		Type:        "bggo-wallpaper",
		APIVersion:  1,
		ID:          id,
		Name:        "Managed Rollback",
		Version:     version,
		Main:        "index.js",
		Permissions: plugin.Permissions{Network: []string{"example.com"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{
		"plugin.json": manifest,
		"index.js": []byte(`export function categories() { return [{ id: "demo", name: "Demo" }]; }
export function list() { return { items: [{ id: "item", title: "` + title + `", fullUrl: "https://example.com/` + title + `.jpg" }] }; }`),
	}
	for name, data := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
