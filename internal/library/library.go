package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const maxRecent = 20

type State struct {
	Recent []string `json:"recent"`
}

type Store struct {
	path string
}

func NewStore() (*Store, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(config, "bggo", "library.json")}, nil
}

func (s *Store) Load() (State, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *Store) Add(path string) (State, error) {
	state, err := s.Load()
	if err != nil {
		return State{}, err
	}
	next := []string{path}
	for _, item := range state.Recent {
		if item != path && len(next) < maxRecent {
			next = append(next, item)
		}
	}
	state.Recent = next
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return State{}, err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return State{}, err
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return State{}, err
	}
	return state, nil
}
