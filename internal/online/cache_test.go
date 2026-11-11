package online

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheManagerSaveAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	cm, err := NewCacheManager(tmpDir)
	if err != nil {
		t.Fatalf("NewCacheManager failed: %v", err)
	}

	key := cm.KeyForURL("https://example.com/image.jpg")
	if key == "" {
		t.Fatalf("expected non-empty key")
	}

	// 1. 缓存未存在
	if _, ok := cm.Get(key, ".jpg"); ok {
		t.Errorf("expected cache miss for non-existent file")
	}

	// 2. 正常流式保存
	data := []byte("fake-image-binary-data")
	savedPath, err := cm.Save(key, ".jpg", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("cm.Save failed: %v", err)
	}

	if !strings.HasSuffix(savedPath, ".jpg") {
		t.Errorf("expected saved path to end with .jpg, got %s", savedPath)
	}

	// 3. 再次获取应当命中缓存
	gotPath, ok := cm.Get(key, ".jpg")
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if gotPath != savedPath {
		t.Errorf("gotPath %s != savedPath %s", gotPath, savedPath)
	}

	// 校验内容
	content, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	if string(content) != string(data) {
		t.Errorf("content mismatch, got %q, want %q", content, data)
	}
}

func TestCacheManagerEmptyData(t *testing.T) {
	tmpDir := t.TempDir()
	cm, err := NewCacheManager(tmpDir)
	if err != nil {
		t.Fatalf("NewCacheManager failed: %v", err)
	}

	key := "empty_test"
	_, err = cm.Save(key, ".jpg", bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatalf("expected error when saving empty reader, got nil")
	}

	// 确保没有残留文件
	if _, ok := cm.Get(key, ".jpg"); ok {
		t.Errorf("expected no file created for empty content")
	}
}

func TestCacheManagerDefaultDir(t *testing.T) {
	cm, err := NewCacheManager("")
	if err != nil {
		t.Fatalf("NewCacheManager with empty dir failed: %v", err)
	}
	if cm.Dir == "" {
		t.Errorf("expected non-empty Dir")
	}
	if !filepath.IsAbs(cm.Dir) {
		t.Errorf("expected absolute path, got %s", cm.Dir)
	}
}
