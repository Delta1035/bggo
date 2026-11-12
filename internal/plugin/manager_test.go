package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestScanDirectoryAndEnabledState(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "scan-demo")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(Manifest{
		Type:       pluginType,
		APIVersion: pluginAPIVersion,
		ID:         "scan-demo",
		Name:       "Scan Demo",
		Version:    "1.0.0",
		Main:       "index.js",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte("export function ok() { return true; }"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := ScanDirectory(root)
	if err != nil {
		t.Fatalf("ScanDirectory() failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Manifest.ID != "scan-demo" || entries[0].Err != nil {
		t.Fatalf("ScanDirectory() = %#v", entries)
	}

	statePath := filepath.Join(root, "plugin-state.json")
	want := EnabledState{"scan-demo": false}
	if err := SaveEnabledState(statePath, want); err != nil {
		t.Fatalf("SaveEnabledState() failed: %v", err)
	}
	got, err := LoadEnabledState(statePath)
	if err != nil {
		t.Fatalf("LoadEnabledState() failed: %v", err)
	}
	if !equalEnabledState(got, want) {
		t.Fatalf("LoadEnabledState() = %#v, want %#v", got, want)
	}
}

func TestCheckCompatibility(t *testing.T) {
	if err := CheckCompatibility(Manifest{MinAppVersion: "0.2.10"}, "v0.2.11"); err != nil {
		t.Fatalf("newer host rejected: %v", err)
	}
	if err := CheckCompatibility(Manifest{MinAppVersion: "0.2.10"}, "v0.2.9"); err == nil {
		t.Fatal("older host was accepted")
	}
	if err := CheckCompatibility(Manifest{MinAppVersion: "0.2"}, "v0.2.10"); err != nil {
		t.Fatalf("short version rejected: %v", err)
	}
}

func equalEnabledState(left, right EnabledState) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range right {
		if left[key] != value {
			return false
		}
	}
	return true
}
