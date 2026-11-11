package online

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CacheManager 负责管理在线壁纸的本地持久化存储
type CacheManager struct {
	Dir string
}

// NewCacheManager 创建缓存管理器
func NewCacheManager(dir string) (*CacheManager, error) {
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			base = os.TempDir()
		}
		dir = filepath.Join(base, "bggo", "wallpapers")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建壁纸缓存目录失败: %w", err)
	}
	return &CacheManager{Dir: dir}, nil
}

// KeyForURL 根据 URL 或 ID 生成安全的文件名
func (c *CacheManager) KeyForURL(rawURL string) string {
	h := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(h[:16])
}

// FilePath 计算缓存文件的完整路径
func (c *CacheManager) FilePath(key, ext string) string {
	if !strings.HasPrefix(ext, ".") && ext != "" {
		ext = "." + ext
	}
	if ext == "" {
		ext = ".jpg"
	}
	return filepath.Join(c.Dir, key+ext)
}

// Get 检查指定 key 是否已有本地缓存文件
func (c *CacheManager) Get(key, ext string) (string, bool) {
	path := c.FilePath(key, ext)
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() && info.Size() > 0 {
		return path, true
	}
	return "", false
}

// Save 将内容流式原子写入缓存文件
func (c *CacheManager) Save(key, ext string, r io.Reader) (string, error) {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", fmt.Errorf("创建缓存目录失败: %w", err)
	}
	targetPath := c.FilePath(key, ext)

	// 创建同一目录下的临时文件，保证跨平台原子 rename
	tmpFile, err := os.CreateTemp(c.Dir, "download-*.tmp")
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		// 如果临时文件仍存在则清理
		_ = os.Remove(tmpName)
	}()

	written, err := io.Copy(tmpFile, r)
	if err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("写入缓存数据失败: %w", err)
	}
	if written == 0 {
		_ = tmpFile.Close()
		return "", errors.New("下载到的文件内容为空")
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("同步文件失败: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("关闭文件失败: %w", err)
	}

	// 原子替换
	if err := os.Rename(tmpName, targetPath); err != nil {
		return "", fmt.Errorf("保存缓存文件失败: %w", err)
	}

	return targetPath, nil
}
