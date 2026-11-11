package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	maxRecent    = 20
	maxFavorites = 50
)

type State struct {
	Recent     []string `json:"recent"`
	Favorites  []string `json:"favorites,omitempty"`
	SyncVSCode bool     `json:"sync_vscode,omitempty"`
	AutoBing   bool     `json:"auto_bing,omitempty"`
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

// NewStoreWithPath 创建指向自定义路径的 Store（主要用于测试与隔离环境）
func NewStoreWithPath(path string) *Store {
	return &Store{path: path}
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
	if err := s.save(state); err != nil {
		return State{}, err
	}
	return state, nil
}

// ClearRecent 清空最近使用壁纸列表
func (s *Store) ClearRecent() (State, error) {
	state, err := s.Load()
	if err != nil {
		return State{}, err
	}
	state.Recent = nil
	if err := s.save(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *Store) SetSyncVSCode(enabled bool) (State, error) {
	state, err := s.Load()
	if err != nil {
		return State{}, err
	}
	state.SyncVSCode = enabled
	if err := s.save(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *Store) SetAutoBing(enabled bool) (State, error) {
	state, err := s.Load()
	if err != nil {
		return State{}, err
	}
	state.AutoBing = enabled
	if err := s.save(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *Store) ToggleFavorite(path string) (State, bool, error) {
	state, err := s.Load()
	if err != nil {
		return State{}, false, err
	}
	exists := false
	var next []string
	for _, item := range state.Favorites {
		if item == path {
			exists = true
		} else {
			next = append(next, item)
		}
	}
	isFavorite := false
	if !exists {
		next = append([]string{path}, next...)
		if len(next) > maxFavorites {
			next = next[:maxFavorites]
		}
		isFavorite = true
	}
	state.Favorites = next
	if err := s.save(state); err != nil {
		return State{}, false, err
	}
	return state, isFavorite, nil
}

func (s *Store) save(state State) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}
