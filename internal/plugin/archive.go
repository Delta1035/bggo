package plugin

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxArchiveFiles       = 128
	maxArchiveFileBytes   = 8 << 20
	maxArchiveTotalBytes  = 32 << 20
	maxManifestBytes      = 256 << 10
	maxPluginIDPathLength = 128
)

// InspectArchive validates a plugin ZIP without writing it to disk.
func InspectArchive(path string) (Manifest, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("打开插件包失败: %w", err)
	}
	defer r.Close()
	return inspectFiles(r.File)
}

// InstallArchive validates and atomically installs a plugin ZIP under root.
// Existing plugin files are replaced only after the complete new directory
// has been extracted and its JavaScript entry has compiled successfully.
func InstallArchive(path, root string) (Manifest, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("打开插件包失败: %w", err)
	}
	defer r.Close()

	manifest, err := inspectFiles(r.File)
	if err != nil {
		return Manifest{}, err
	}
	if len(manifest.ID) > maxPluginIDPathLength {
		return Manifest{}, errors.New("插件 ID 过长")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Manifest{}, fmt.Errorf("创建插件目录失败: %w", err)
	}
	staging, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return Manifest{}, fmt.Errorf("创建插件临时目录失败: %w", err)
	}
	defer os.RemoveAll(staging)

	if err := extractFiles(r.File, staging); err != nil {
		return Manifest{}, err
	}
	if _, err := compileEntry(staging, manifest); err != nil {
		return Manifest{}, fmt.Errorf("插件入口校验失败: %w", err)
	}

	target := filepath.Join(root, manifest.ID)
	if err := ensureChild(root, target); err != nil {
		return Manifest{}, err
	}
	var previous string
	if _, err := os.Stat(target); err == nil {
		previousVersion := manifest.Version
		if currentManifest, inspectErr := InspectDirectory(target); inspectErr == nil {
			previousVersion = currentManifest.Version
		}
		historyDir := filepath.Join(root, ".history", manifest.ID)
		if err := os.MkdirAll(historyDir, 0o700); err != nil {
			return Manifest{}, fmt.Errorf("创建插件历史目录失败: %w", err)
		}
		previous = filepath.Join(historyDir, historyName(previousVersion))
		if err := os.Rename(target, previous); err != nil {
			return Manifest{}, fmt.Errorf("备份旧插件失败: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, fmt.Errorf("检查旧插件失败: %w", err)
	}
	if err := os.Rename(staging, target); err != nil {
		if previous != "" {
			_ = os.Rename(previous, target)
		}
		return Manifest{}, fmt.Errorf("安装插件失败: %w", err)
	}
	return manifest, nil
}

// VersionRecord describes a previous installed version kept for rollback.
type VersionRecord struct {
	ID       string
	Manifest Manifest
	Dir      string
}

// ListVersions returns previous versions from newest to oldest.
func ListVersions(root, id string) ([]VersionRecord, error) {
	if !pluginIDPattern.MatchString(id) {
		return nil, fmt.Errorf("无效的插件 ID: %q", id)
	}
	historyRoot := filepath.Join(root, ".history", id)
	entries, err := os.ReadDir(historyRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	versions := make([]VersionRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		dir := filepath.Join(historyRoot, entry.Name())
		manifest, inspectErr := InspectDirectory(dir)
		if inspectErr != nil {
			continue
		}
		versions = append(versions, VersionRecord{ID: entry.Name(), Manifest: manifest, Dir: dir})
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].ID > versions[j].ID })
	return versions, nil
}

// Rollback swaps the active package with one of its previous versions.
func Rollback(root, id, versionID string) (Manifest, error) {
	if !pluginIDPattern.MatchString(id) || versionID == "" || filepath.Base(versionID) != versionID {
		return Manifest{}, errors.New("无效的插件版本路径")
	}
	target := filepath.Join(root, id)
	previous := filepath.Join(root, ".history", id, versionID)
	if err := ensureChild(root, target); err != nil {
		return Manifest{}, err
	}
	if err := ensureChild(filepath.Join(root, ".history", id), previous); err != nil {
		return Manifest{}, err
	}
	manifest, err := InspectDirectory(previous)
	if err != nil {
		return Manifest{}, fmt.Errorf("历史插件版本校验失败: %w", err)
	}
	if _, err := os.Stat(target); err != nil {
		return Manifest{}, fmt.Errorf("当前插件不存在: %w", err)
	}
	historyTarget := filepath.Join(root, ".history", id, historyName(manifest.Version))
	if err := os.Rename(target, historyTarget); err != nil {
		return Manifest{}, fmt.Errorf("保存当前插件版本失败: %w", err)
	}
	if err := os.Rename(previous, target); err != nil {
		_ = os.Rename(historyTarget, target)
		return Manifest{}, fmt.Errorf("切换历史插件版本失败: %w", err)
	}
	return manifest, nil
}

func historyName(version string) string {
	version = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(version)
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + version
}

// RemoveInstalled removes only the plugin package. Runtime data is kept in a
// separate data directory so uninstall can be reversed by reinstalling it.
func RemoveInstalled(root, id string) error {
	if !pluginIDPattern.MatchString(id) {
		return fmt.Errorf("无效的插件 ID: %q", id)
	}
	target := filepath.Join(root, id)
	if err := ensureChild(root, target); err != nil {
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("卸载插件失败: %w", err)
	}
	_ = os.RemoveAll(filepath.Join(root, ".history", id))
	return nil
}

func inspectFiles(files []*zip.File) (Manifest, error) {
	if len(files) == 0 || len(files) > maxArchiveFiles {
		return Manifest{}, fmt.Errorf("插件包文件数量必须在 1 到 %d 之间", maxArchiveFiles)
	}
	manifestPath, prefix, err := archiveManifestPath(files)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	var total uint64
	for _, file := range files {
		name, err := archiveRelativePath(file.Name, prefix)
		if err != nil {
			return Manifest{}, err
		}
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return Manifest{}, fmt.Errorf("插件包禁止符号链接: %q", file.Name)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.FileInfo().Mode().IsRegular() {
			return Manifest{}, fmt.Errorf("插件包包含不支持的文件类型: %q", file.Name)
		}
		if file.UncompressedSize64 > maxArchiveFileBytes {
			return Manifest{}, fmt.Errorf("插件包文件过大: %q", name)
		}
		total += file.UncompressedSize64
		if total > maxArchiveTotalBytes {
			return Manifest{}, errors.New("插件包解压后总大小超过限制")
		}
		if name != manifestPath {
			continue
		}
		if file.UncompressedSize64 > maxManifestBytes {
			return Manifest{}, errors.New("插件 manifest 过大")
		}
		reader, err := file.Open()
		if err != nil {
			return Manifest{}, fmt.Errorf("读取插件 manifest 失败: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maxManifestBytes+1))
		_ = reader.Close()
		if readErr != nil {
			return Manifest{}, fmt.Errorf("读取插件 manifest 失败: %w", readErr)
		}
		if len(data) > maxManifestBytes {
			return Manifest{}, errors.New("插件 manifest 过大")
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			return Manifest{}, fmt.Errorf("解析插件 manifest 失败: %w", err)
		}
	}
	if manifestPath == "" {
		return Manifest{}, errors.New("插件包缺少 plugin.json")
	}
	if err := validateManifest(&manifest); err != nil {
		return Manifest{}, err
	}
	entry := filepath.ToSlash(filepath.Clean(manifest.Main))
	if _, err := findArchiveFile(files, prefix, entry); err != nil {
		return Manifest{}, fmt.Errorf("插件入口不存在: %w", err)
	}
	return manifest, nil
}

func extractFiles(files []*zip.File, destination string) error {
	_, prefix, err := archiveManifestPath(files)
	if err != nil {
		return err
	}
	var totalBytes uint64
	for _, file := range files {
		rel, err := archiveRelativePath(file.Name, prefix)
		if err != nil {
			return err
		}
		if rel == "" {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(rel))
		if err := ensureChild(destination, target); err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return fmt.Errorf("创建插件目录失败: %w", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("创建插件目录失败: %w", err)
		}
		reader, err := file.Open()
		if err != nil {
			return fmt.Errorf("读取插件文件失败: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maxArchiveFileBytes+1))
		_ = reader.Close()
		if readErr != nil {
			return fmt.Errorf("读取插件文件失败: %w", readErr)
		}
		if len(data) > maxArchiveFileBytes {
			return fmt.Errorf("插件包文件过大: %q", rel)
		}
		totalBytes += uint64(len(data))
		if totalBytes > maxArchiveTotalBytes {
			return errors.New("插件包解压后总大小超过限制")
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return fmt.Errorf("写入插件文件失败: %w", err)
		}
	}
	return nil
}

func archiveManifestPath(files []*zip.File) (string, string, error) {
	var candidates []string
	for _, file := range files {
		name := strings.TrimPrefix(filepath.ToSlash(file.Name), "./")
		if name == "" {
			continue
		}
		if _, err := archiveRelativePath(name, ""); err != nil {
			return "", "", err
		}
		if filepath.Base(name) == "plugin.json" {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) != 1 {
		return "", "", errors.New("插件包必须包含且只能包含一个 plugin.json")
	}
	manifestPath := candidates[0]
	prefix := strings.TrimSuffix(filepath.ToSlash(filepath.Dir(manifestPath)), "/")
	if prefix == "." {
		prefix = ""
	}
	for _, file := range files {
		if _, err := archiveRelativePath(file.Name, prefix); err != nil {
			return "", "", err
		}
	}
	return strings.TrimPrefix(manifestPath, prefix+"/"), prefix, nil
}

func archiveRelativePath(raw, prefix string) (string, error) {
	name := filepath.ToSlash(strings.TrimPrefix(raw, "./"))
	if strings.Contains(name, "\\") || filepath.IsAbs(name) || name == "" {
		if name == "" {
			return "", nil
		}
		return "", fmt.Errorf("插件包包含非法路径: %q", raw)
	}
	clean := pathCleanSlash(name)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("插件包包含路径穿越: %q", raw)
	}
	if prefix != "" {
		prefix = pathCleanSlash(prefix)
		if clean == prefix {
			return "", nil
		}
		if !strings.HasPrefix(clean, prefix+"/") {
			return "", fmt.Errorf("插件包包含多个顶层目录: %q", raw)
		}
		clean = strings.TrimPrefix(clean, prefix+"/")
	}
	return clean, nil
}

func pathCleanSlash(name string) string {
	parts := strings.Split(name, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
		case "..":
			if len(clean) > 0 {
				clean = clean[:len(clean)-1]
			} else {
				return ".."
			}
		default:
			clean = append(clean, part)
		}
	}
	return strings.Join(clean, "/")
}

func findArchiveFile(files []*zip.File, prefix, path string) (*zip.File, error) {
	for _, file := range files {
		rel, err := archiveRelativePath(file.Name, prefix)
		if err != nil {
			return nil, err
		}
		if rel == path {
			return file, nil
		}
	}
	return nil, fmt.Errorf("文件 %q 不存在", path)
}
