package main

import (
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
