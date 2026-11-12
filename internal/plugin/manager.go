package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// DirectoryPlugin is the validated metadata for a plugin installed on disk.
// Loading metadata does not execute the plugin runtime.
type DirectoryPlugin struct {
	Manifest Manifest
	Dir      string
	Err      error
}

// InspectDirectory validates a plugin directory and compiles its entry point
// without starting a runtime.
func InspectDirectory(dir string) (Manifest, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return Manifest{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Manifest{}, errors.New("插件目录必须是普通目录")
	}
	manifest, err := readManifest(dir)
	if err != nil {
		return Manifest{}, err
	}
	if _, err := compileEntry(dir, manifest); err != nil {
		return Manifest{}, fmt.Errorf("插件入口校验失败: %w", err)
	}
	return manifest, nil
}

// ScanDirectory finds plugin directories and returns invalid entries as
// records with Err so the management UI can show actionable failures.
func ScanDirectory(root string) ([]DirectoryPlugin, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]DirectoryPlugin, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			dir := filepath.Join(root, entry.Name())
			manifest, inspectErr := InspectDirectory(dir)
			result = append(result, DirectoryPlugin{Manifest: manifest, Dir: dir, Err: inspectErr})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left := result[i].Manifest.ID
		if left == "" {
			left = filepath.Base(result[i].Dir)
		}
		right := result[j].Manifest.ID
		if right == "" {
			right = filepath.Base(result[j].Dir)
		}
		return left < right
	})
	return result, nil
}

// EnabledState stores the user's enable/disable decision per plugin ID.
type EnabledState map[string]bool

func LoadEnabledState(path string) (EnabledState, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return EnabledState{}, nil
	}
	if err != nil {
		return nil, err
	}
	var state EnabledState
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, fmt.Errorf("解析插件状态失败: %w", err)
	}
	return state, nil
}

func SaveEnabledState(path string, state EnabledState) error {
	if state == nil {
		state = EnabledState{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
