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
