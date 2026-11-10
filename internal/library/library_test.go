package library

import (
	"path/filepath"
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
