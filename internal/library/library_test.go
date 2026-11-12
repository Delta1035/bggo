package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddMovesExistingItemToFront(t *testing.T) {
	s := &Store{path: filepath.Join(t.TempDir(), "library.json")}
	for _, path := range []string{"one.png", "two.png", "one.png"} {
		if _, err := s.Add(path); err != nil {
			t.Fatal(err)
		}
	}
	state, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Recent) != 2 || state.Recent[0] != "one.png" || state.Recent[1] != "two.png" {
		t.Fatalf("recent = %#v", state.Recent)
	}
}

func TestSetSyncVSCode(t *testing.T) {
	s := &Store{path: filepath.Join(t.TempDir(), "library.json")}
	state, err := s.SetSyncVSCode(true)
	if err != nil {
		t.Fatalf("SetSyncVSCode(true) error: %v", err)
	}
	if !state.SyncVSCode {
		t.Errorf("expected state.SyncVSCode=true")
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !loaded.SyncVSCode {
		t.Errorf("expected loaded.SyncVSCode=true")
	}

	state, err = s.SetSyncVSCode(false)
	if err != nil {
		t.Fatalf("SetSyncVSCode(false) error: %v", err)
	}
	if state.SyncVSCode {
		t.Errorf("expected state.SyncVSCode=false")
	}
}

func TestSetAutoBing(t *testing.T) {
	s := &Store{path: filepath.Join(t.TempDir(), "library.json")}
	state, err := s.SetAutoBing(true)
	if err != nil {
		t.Fatalf("SetAutoBing(true) error: %v", err)
	}
	if !state.AutoBing {
		t.Errorf("expected state.AutoBing=true")
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !loaded.AutoBing {
		t.Errorf("expected loaded.AutoBing=true")
	}

	state, err = s.SetAutoBing(false)
	if err != nil {
		t.Fatalf("SetAutoBing(false) error: %v", err)
	}
	if state.AutoBing {
		t.Errorf("expected state.AutoBing=false")
	}
}

func TestToggleFavorite(t *testing.T) {
	s := &Store{path: filepath.Join(t.TempDir(), "library.json")}

	// 1. 首次添加收藏
	state, isFav, err := s.ToggleFavorite("fav1.jpg")
	if err != nil {
		t.Fatalf("ToggleFavorite error: %v", err)
	}
	if !isFav {
		t.Errorf("expected isFav=true on first add")
	}
	if len(state.Favorites) != 1 || state.Favorites[0] != "fav1.jpg" {
		t.Errorf("unexpected favorites: %#v", state.Favorites)
	}

	// 2. 再次切换同一路径：取消收藏
	state, isFav, err = s.ToggleFavorite("fav1.jpg")
	if err != nil {
		t.Fatalf("ToggleFavorite remove error: %v", err)
	}
	if isFav {
		t.Errorf("expected isFav=false on removal")
	}
	if len(state.Favorites) != 0 {
		t.Errorf("expected empty favorites, got %#v", state.Favorites)
	}
}

func TestClearRecent(t *testing.T) {
	s := &Store{path: filepath.Join(t.TempDir(), "library.json")}
	if _, err := s.Add("wall1.png"); err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if _, err := s.Add("wall2.png"); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	state, err := s.ClearRecent()
	if err != nil {
		t.Fatalf("ClearRecent error: %v", err)
	}
	if len(state.Recent) != 0 {
		t.Errorf("expected state.Recent to be empty, got %#v", state.Recent)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(loaded.Recent) != 0 {
		t.Errorf("expected loaded.Recent to be empty, got %#v", loaded.Recent)
	}
}

func TestLoadBacksUpCorruptedState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "library.json")
	if err := os.WriteFile(path, []byte(`{"recent":`), 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewStoreWithPath(path)
	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if state.Notice == "" {
		t.Fatal("expected a recovery notice")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected corrupted file to be moved, stat error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var backupFound bool
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "library.json.corrupt-") {
			backupFound = true
			break
		}
	}
	if !backupFound {
		t.Fatal("expected a backup of the corrupted configuration")
	}

	if _, err := store.Add("recovered.png"); err != nil {
		t.Fatalf("Add() after recovery error = %v", err)
	}
}
