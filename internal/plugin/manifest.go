package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/MoeForge/moejs"
)

const (
	pluginType       = "bggo-wallpaper"
	pluginAPIVersion = 1
)

var pluginIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Permissions declares the capabilities a plugin may use.
type Permissions struct {
	Network []string `json:"network,omitempty"`
}

// Setting describes one user-visible plugin setting.
type Setting struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Help  string `json:"help,omitempty"`
	Type  string `json:"type,omitempty"`
	Link  string `json:"link,omitempty"`
}

// Manifest is the plugin.json contract for wallpaper sources.
type Manifest struct {
	Type          string      `json:"type"`
	APIVersion    int         `json:"apiVersion"`
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Version       string      `json:"version"`
	MinAppVersion string      `json:"minAppVersion,omitempty"`
	Main          string      `json:"main,omitempty"`
	Permissions   Permissions `json:"permissions,omitempty"`
	Settings      []Setting   `json:"settings,omitempty"`
}

// Category is the JSON representation returned by a plugin.
type Category struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Badge       string `json:"badge,omitempty"`
}

// WallpaperItem is the JSON representation returned by a plugin.
type WallpaperItem struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Author     string `json:"author,omitempty"`
	ThumbURL   string `json:"thumbUrl"`
	FullURL    string `json:"fullUrl"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Resolution string `json:"resolution,omitempty"`
	Copyright  string `json:"copyright,omitempty"`
}

// ListResult is the result of the plugin list export.
type ListResult struct {
	Items []WallpaperItem `json:"items"`
	More  bool            `json:"more,omitempty"`
	Total int             `json:"total,omitempty"`
}

// ErrUnsupported means the plugin does not export a requested capability.
var ErrUnsupported = errors.New("插件不支持此能力")

func readManifest(dir string) (Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("解析插件 manifest 失败: %w", err)
	}
	if err := validateManifest(&m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func validateManifest(m *Manifest) error {
	if m.Type != pluginType {
		return fmt.Errorf("插件类型必须为 %q", pluginType)
	}
	if m.APIVersion != pluginAPIVersion {
		return fmt.Errorf("不支持的插件 API 版本: %d", m.APIVersion)
	}
	if !pluginIDPattern.MatchString(m.ID) {
		return fmt.Errorf("无效的插件 ID: %q", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("插件名称不能为空")
	}
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("插件版本不能为空")
	}
	if m.Main == "" {
		m.Main = "index.js"
	}
	if !filepath.IsLocal(m.Main) {
		return fmt.Errorf("插件入口必须位于插件目录内: %q", m.Main)
	}
	for _, host := range m.Permissions.Network {
		if strings.TrimSpace(host) == "" || strings.ContainsAny(host, "/\\:@") {
			return fmt.Errorf("无效的网络权限主机: %q", host)
		}
	}
	return nil
}

// CheckCompatibility rejects plugins that require a newer host version.
func CheckCompatibility(m Manifest, appVersion string) error {
	if m.MinAppVersion == "" {
		return nil
	}
	minimum, err := versionParts(m.MinAppVersion)
	if err != nil {
		return fmt.Errorf("插件最低应用版本无效: %w", err)
	}
	current, err := versionParts(appVersion)
	if err != nil {
		return fmt.Errorf("当前应用版本无效: %w", err)
	}
	for i := 0; i < len(minimum); i++ {
		if current[i] > minimum[i] {
			return nil
		}
		if current[i] < minimum[i] {
			return fmt.Errorf("插件需要 BGGo %s 或更高版本", strings.TrimPrefix(m.MinAppVersion, "v"))
		}
	}
	return nil
}

func versionParts(value string) ([3]int, error) {
	var result [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.SplitN(value, ".", 4)
	if len(parts) < 2 {
		return result, fmt.Errorf("版本号格式错误: %q", value)
	}
	for i := range result {
		part := "0"
		if i < len(parts) {
			part = parts[i]
		}
		if dash := strings.IndexByte(part, '-'); dash >= 0 {
			part = part[:dash]
		}
		if part == "" {
			return result, fmt.Errorf("版本号格式错误: %q", value)
		}
		number, err := strconv.Atoi(part)
		if err != nil {
			return result, fmt.Errorf("版本号格式错误: %q", value)
		}
		result[i] = number
	}
	return result, nil
}

func compileEntry(dir string, m Manifest) (*moejs.Module, error) {
	entry := filepath.Join(dir, m.Main)
	if err := ensureChild(dir, entry); err != nil {
		return nil, err
	}
	info, err := os.Lstat(entry)
	if err != nil {
		return nil, fmt.Errorf("读取插件入口失败: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("插件入口必须是普通文件")
	}
	source, err := os.ReadFile(entry)
	if err != nil {
		return nil, fmt.Errorf("读取插件入口失败: %w", err)
	}
	if strings.HasSuffix(strings.ToLower(m.Main), ".ts") {
		return moejs.CompileTS(m.Main, string(source))
	}
	return moejs.Compile(m.Main, string(source))
}

func ensureChild(root, target string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("插件入口超出插件目录: %q", target)
	}
	return nil
}
