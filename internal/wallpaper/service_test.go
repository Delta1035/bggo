package wallpaper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceRejectsUnsupportedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := (Service{}).Set(path)
	if err == nil || !strings.Contains(err.Error(), "不支持的图片格式") {
		t.Fatalf("Set() error = %v", err)
	}
}

func TestServiceRejectsDirectory(t *testing.T) {
	err := (Service{}).Set(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "不是目录") && !strings.Contains(err.Error(), "而不是目录") {
		t.Fatalf("Set() error = %v", err)
	}
}
