package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginRunsRealJavaScriptAndPersistsStorage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"remote-1","title":"Remote","fullUrl":"https://images.example/1.jpg"}]}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	manifest := Manifest{
		Type:       pluginType,
		APIVersion: pluginAPIVersion,
		ID:         "demo",
		Name:       "Demo",
		Version:    "1.0.0",
		Main:       "index.js",
		Permissions: Permissions{
			Network: []string{"127.0.0.1"},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), manifestJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	source := `
export function categories() {
  return [{ id: "demo", name: host.setting("name") || "Demo" }];
}

export async function list({ categoryId }) {
  const response = await host.fetch("` + server.URL + `");
  const result = await response.json();
  host.storage.set("lastCategory", categoryId);
  return result;
}

export function saved() {
  return host.storage.get("lastCategory") || null;
}
`
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	p, err := Load(dir, dataDir)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if err := p.SetSetting("name", "Configured"); err != nil {
		t.Fatalf("SetSetting() failed: %v", err)
	}

	var categories []Category
	if err := p.Call(context.Background(), "categories", map[string]any{}, &categories); err != nil {
		t.Fatalf("categories() failed: %v", err)
	}
	if len(categories) != 1 || categories[0].Name != "Configured" {
		t.Fatalf("categories() = %#v", categories)
	}

	var result ListResult
	if err := p.Call(context.Background(), "list", map[string]any{"categoryId": "demo"}, &result); err != nil {
		t.Fatalf("list() failed: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "remote-1" {
		t.Fatalf("list() = %#v", result)
	}
	p.Close()

	p, err = Load(dir, dataDir)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	defer p.Close()
	var saved string
	if err := p.Call(context.Background(), "saved", nil, &saved); err != nil {
		t.Fatalf("saved() failed: %v", err)
	}
	if saved != "demo" {
		t.Fatalf("saved() = %q, want demo", saved)
	}
}

func TestLoadSourceUsesThePluginRuntime(t *testing.T) {
	manifest := Manifest{
		Type:       pluginType,
		APIVersion: pluginAPIVersion,
		ID:         "embedded-demo",
		Name:       "Embedded Demo",
		Version:    "1.0.0",
		Main:       "index.js",
	}
	p, err := LoadSource(manifest, "index.js", `
export function categories() {
  return [{ id: "daily", name: host.setting("label") || "Daily" }];
}
`, t.TempDir())
	if err != nil {
		t.Fatalf("LoadSource() failed: %v", err)
	}
	defer p.Close()
	if err := p.SetSetting("label", "Embedded"); err != nil {
		t.Fatalf("SetSetting() failed: %v", err)
	}
	var categories []Category
	if err := p.Call(context.Background(), "categories", nil, &categories); err != nil {
		t.Fatalf("categories() failed: %v", err)
	}
	if len(categories) != 1 || categories[0].Name != "Embedded" {
		t.Fatalf("categories() = %#v", categories)
	}
}
