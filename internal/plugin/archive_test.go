package plugin

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallArchiveValidatesAndRunsPlugin(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "demo.zip")
	manifest := Manifest{
		Type:       pluginType,
		APIVersion: pluginAPIVersion,
		ID:         "archive-demo",
		Name:       "Archive Demo",
		Version:    "1.0.0",
		Main:       "index.js",
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeZip(t, archivePath, map[string][]byte{
		"package/plugin.json": manifestJSON,
		"package/index.js":    []byte(`export function name() { return "archive"; }`),
	})

	root := filepath.Join(t.TempDir(), "plugins")
	got, err := InspectArchive(archivePath)
	if err != nil {
		t.Fatalf("InspectArchive() failed: %v", err)
	}
	if got.ID != manifest.ID {
		t.Fatalf("InspectArchive() ID = %q, want %q", got.ID, manifest.ID)
	}
	if _, err := InstallArchive(archivePath, root); err != nil {
		t.Fatalf("InstallArchive() failed: %v", err)
	}

	installed, err := Load(filepath.Join(root, manifest.ID), t.TempDir())
	if err != nil {
		t.Fatalf("Load(installed) failed: %v", err)
	}
	defer installed.Close()
	var name string
	if err := installed.Call(context.Background(), "name", nil, &name); err != nil {
		t.Fatalf("installed plugin call failed: %v", err)
	}
	if name != "archive" {
		t.Fatalf("installed plugin returned %q, want archive", name)
	}

	if err := RemoveInstalled(root, manifest.ID); err != nil {
		t.Fatalf("RemoveInstalled() failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, manifest.ID)); !os.IsNotExist(err) {
		t.Fatalf("plugin directory still exists, stat error = %v", err)
	}
}

func TestInspectArchiveRejectsUnsafeEntries(t *testing.T) {
	validManifest := []byte(`{"type":"bggo-wallpaper","apiVersion":1,"id":"unsafe-demo","name":"Unsafe","version":"1.0.0","main":"index.js"}`)
	tests := []struct {
		name  string
		files map[string][]byte
	}{
		{name: "path traversal", files: map[string][]byte{
			"plugin.json": validManifest,
			"index.js":    []byte("export function ok() { return true; }"),
			"../outside":  []byte("nope"),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "unsafe.zip")
			writeZip(t, path, tt.files)
			if _, err := InspectArchive(path); err == nil {
				t.Fatal("InspectArchive() succeeded for unsafe archive")
			}
		})
	}

	symlinkPath := filepath.Join(t.TempDir(), "symlink.zip")
	file, err := os.Create(symlinkPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	addZipBytes(t, writer, "plugin.json", validManifest)
	header := &zip.FileHeader{Name: "index.js", Method: zip.Deflate}
	header.SetMode(os.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("/outside")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectArchive(symlinkPath); err == nil {
		t.Fatal("InspectArchive() succeeded for symlink archive")
	}
}

func writeZip(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, data := range files {
		addZipBytes(t, writer, name, data)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func addZipBytes(t *testing.T, writer *zip.Writer, name string, data []byte) {
	t.Helper()
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(data); err != nil {
		t.Fatal(err)
	}
}
