package vscode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	SettingWallpaperPath    = "bggo.wallpaper.path"
	SettingOpacity          = "bggo.wallpaper.opacity"
	SettingBlur             = "bggo.wallpaper.blur"
	SettingPosition         = "bggo.wallpaper.position"
	SettingCustomCSSImports = "vscode_custom_css.imports"

	DefaultOpacity  = 0.16
	DefaultBlur     = 0
	DefaultPosition = "center"

	ManagedExtensionID = "bggo.bggo-wallpaper"
	ManagedScriptFile  = "bggo-wallpaper.js"
	ManagedStyleFile   = "bggo-wallpaper.css"
)

// CommandRunner defines the function signature for executing system commands.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ApplyReport holds the summary of mapping a wallpaper into VS Code.
type ApplyReport struct {
	UserDir            string
	WallpaperPath      string
	SettingsPath       string
	ScriptPath         string
	StylePath          string
	ExtensionInstalled bool
}

// Service provides methods to detect VS Code and apply wallpaper settings.
type Service struct {
	UserDir       string        // If set, overrides the auto-detected VS Code User directory.
	ExtensionDir  string        // If set, overrides the auto-detected VS Code extensions directory.
	DownloadURL   string        // If set, overrides the default VS Code extension download URL.
	VSCodeCLIPath string        // If set, overrides the auto-detected 'code' executable path.
	HTTPClient    *http.Client  // If set, overrides the default HTTP client used for downloads.
	CommandRunner CommandRunner // If set, overrides the default exec.Command runner.
}

// Apply maps the given wallpaper into the VS Code configuration and global storage.
func (s Service) Apply(wallpaperPath string) (*ApplyReport, error) {
	abs, err := filepath.Abs(wallpaperPath)
	if err != nil {
		return nil, fmt.Errorf("解析图片路径: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("读取图片: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("请选择图片文件，而不是目录")
	}
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".bmp", ".gif":
	default:
		return nil, fmt.Errorf("不支持的图片格式 %q", filepath.Ext(abs))
	}

	userDir, err := s.resolveUserDir()
	if err != nil {
		return nil, err
	}

	settingsPath := filepath.Join(userDir, "settings.json")
	settings, err := readSettings(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("读取 VS Code 配置: %w", err)
	}

	storageDir := filepath.Join(userDir, "globalStorage", ManagedExtensionID)
	scriptPath := filepath.Join(storageDir, ManagedScriptFile)
	scriptURI := FileURI(scriptPath)
	legacyStyleURI := FileURI(filepath.Join(storageDir, ManagedStyleFile))

	// Update settings map
	settings[SettingWallpaperPath] = abs

	// Manage vscode_custom_css.imports
	imports := extractImports(settings[SettingCustomCSSImports])
	imports = updateImports(imports, scriptURI, legacyStyleURI)
	settings[SettingCustomCSSImports] = imports

	// Retrieve styling preferences from existing configuration if present
	opacity := extractFloat(settings[SettingOpacity], DefaultOpacity)
	blur := extractInt(settings[SettingBlur], DefaultBlur)
	position := extractString(settings[SettingPosition], DefaultPosition)

	// Write settings.json safely
	settingsData, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化 VS Code 配置: %w", err)
	}
	if err := atomicWriteFile(settingsPath, settingsData, 0o644); err != nil {
		return nil, fmt.Errorf("写入 VS Code 配置: %w", err)
	}

	// Generate and write bggo-wallpaper.css
	imageURI := WorkbenchImageURI(abs)
	stylePath := filepath.Join(storageDir, ManagedStyleFile)
	styleContent := GenerateStylesheet(imageURI, opacity, blur, position)
	if err := atomicWriteFile(stylePath, []byte(styleContent), 0o644); err != nil {
		return nil, fmt.Errorf("写入 VS Code 壁纸样式: %w", err)
	}

	// Generate and write bggo-wallpaper.js (for backward compatibility)
	scriptContent := GenerateScript(imageURI, opacity, blur, position)
	if err := atomicWriteFile(scriptPath, []byte(scriptContent), 0o644); err != nil {
		return nil, fmt.Errorf("写入 VS Code 壁纸脚本: %w", err)
	}

	installed := s.IsExtensionInstalled(userDir)

	return &ApplyReport{
		UserDir:            userDir,
		WallpaperPath:      abs,
		SettingsPath:       settingsPath,
		ScriptPath:         scriptPath,
		StylePath:          stylePath,
		ExtensionInstalled: installed,
	}, nil
}

// IsExtensionInstalled checks if BGGo wallpaper extension exists in VS Code extensions directory.
func (s Service) IsExtensionInstalled(userDir string) bool {
	// Check extensions directories
	extDirs := s.candidateExtensionDirs()
	for _, dir := range extDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(strings.ToLower(entry.Name()), ManagedExtensionID) {
				return true
			}
		}
	}
	return false
}

// FindVSCodeCLI finds the executable path for the VS Code command line interface.
func (s Service) FindVSCodeCLI() (string, error) {
	if s.VSCodeCLIPath != "" {
		if _, err := os.Stat(s.VSCodeCLIPath); err == nil {
			return s.VSCodeCLIPath, nil
		}
		if path, err := exec.LookPath(s.VSCodeCLIPath); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("指定的 VS Code CLI 不存在: %s", s.VSCodeCLIPath)
	}

	candidates := []string{"code"}
	if runtime.GOOS == "windows" {
		candidates = []string{"code.cmd", "code.exe", "code"}
	}
	candidates = append(candidates, "code-insiders", "codium")
	if runtime.GOOS == "windows" {
		candidates = append(candidates, "code-insiders.cmd", "codium.cmd")
	}

	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("未检测到 VS Code 命令行工具 (code)。请确保已安装 VS Code 并将其添加到环境变量 PATH")
}

// DownloadAndInstallExtension downloads the VS Code extension vsix package and installs it.
func (s Service) DownloadAndInstallExtension(ctx context.Context) error {
	cliPath, err := s.FindVSCodeCLI()
	if err != nil {
		return err
	}

	downloadURL := s.DownloadURL
	if downloadURL == "" {
		downloadURL = "https://github.com/Delta1035/bggo/releases/latest/download/bggo-wallpaper.vsix"
	}

	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("创建下载请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "BGGo-Desktop-Updater")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载 VS Code 插件失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 VS Code 插件失败，状态码 %d", resp.StatusCode)
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil || cacheDir == "" {
		cacheDir = os.TempDir()
	}
	targetDir := filepath.Join(cacheDir, "bggo")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("创建插件缓存目录失败: %w", err)
	}
	vsixPath := filepath.Join(targetDir, "bggo-wallpaper.vsix")

	outFile, err := os.Create(vsixPath)
	if err != nil {
		return fmt.Errorf("创建插件临时文件失败: %w", err)
	}
	if _, err := io.Copy(outFile, resp.Body); err != nil {
		_ = outFile.Close()
		return fmt.Errorf("写入插件文件失败: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return fmt.Errorf("完成插件文件保存失败: %w", err)
	}

	runner := s.CommandRunner
	if runner == nil {
		runner = func(c context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(c, name, args...)
			return cmd.CombinedOutput()
		}
	}

	output, err := runner(ctx, cliPath, "--install-extension", vsixPath, "--force")
	if err != nil {
		return fmt.Errorf("安装插件失败: %w (输出: %s)", err, strings.TrimSpace(string(output)))
	}

	return nil
}

func (s Service) resolveUserDir() (string, error) {
	if s.UserDir != "" {
		if err := os.MkdirAll(s.UserDir, 0o755); err != nil {
			return "", err
		}
		return s.UserDir, nil
	}
	candidates := candidateUserDirs()
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, nil
		}
	}
	if len(candidates) > 0 {
		target := candidates[0]
		if err := os.MkdirAll(target, 0o755); err != nil {
			return "", fmt.Errorf("创建 VS Code 配置目录失败: %w", err)
		}
		return target, nil
	}
	return "", errors.New("未找到可用的 VS Code 配置目录")
}

func candidateUserDirs() []string {
	var dirs []string
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			dirs = append(dirs,
				filepath.Join(appData, "Code", "User"),
				filepath.Join(appData, "Code - Insiders", "User"),
				filepath.Join(appData, "VSCodium", "User"),
			)
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err == nil {
			base := filepath.Join(home, "Library", "Application Support")
			dirs = append(dirs,
				filepath.Join(base, "Code", "User"),
				filepath.Join(base, "Code - Insiders", "User"),
				filepath.Join(base, "VSCodium", "User"),
			)
		}
	default: // linux, freebsd, etc.
		cfg, err := os.UserConfigDir()
		if err == nil {
			dirs = append(dirs,
				filepath.Join(cfg, "Code", "User"),
				filepath.Join(cfg, "Code - Insiders", "User"),
				filepath.Join(cfg, "VSCodium", "User"),
				filepath.Join(cfg, "Code - OSS", "User"),
			)
		}
	}
	return dirs
}

func (s Service) candidateExtensionDirs() []string {
	if s.ExtensionDir != "" {
		return []string{s.ExtensionDir}
	}
	var dirs []string
	home, err := os.UserHomeDir()
	if err == nil {
		dirs = append(dirs,
			filepath.Join(home, ".vscode", "extensions"),
			filepath.Join(home, ".vscode-insiders", "extensions"),
			filepath.Join(home, ".vscode-oss", "extensions"),
		)
	}
	return dirs
}

func readSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]any), nil
	}
	if err != nil {
		return nil, err
	}
	cleaned := cleanJSONC(data)
	if len(bytes.TrimSpace(cleaned)) == 0 {
		return make(map[string]any), nil
	}
	var res map[string]any
	if err := json.Unmarshal(cleaned, &res); err != nil {
		return nil, err
	}
	if res == nil {
		res = make(map[string]any)
	}
	return res, nil
}

func cleanJSONC(data []byte) []byte {
	var out bytes.Buffer
	inString := false
	inLineComment := false
	inBlockComment := false
	escaped := false

	n := len(data)
	for i := 0; i < n; i++ {
		ch := data[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
				out.WriteByte(ch)
			}
			continue
		}
		if inBlockComment {
			if ch == '*' && i+1 < n && data[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			escaped = false
			out.WriteByte(ch)
			continue
		}

		if ch == '/' && i+1 < n {
			next := data[i+1]
			if next == '/' {
				inLineComment = true
				i++
				continue
			} else if next == '*' {
				inBlockComment = true
				i++
				continue
			}
		}

		out.WriteByte(ch)
	}

	// Remove trailing commas before } or ]
	raw := out.Bytes()
	var finalOut bytes.Buffer
	for i := 0; i < len(raw); i++ {
		if raw[i] == ',' {
			// Look ahead for closing brace or bracket, skipping whitespace
			j := i + 1
			for j < len(raw) && (raw[j] == ' ' || raw[j] == '\t' || raw[j] == '\r' || raw[j] == '\n') {
				j++
			}
			if j < len(raw) && (raw[j] == '}' || raw[j] == ']') {
				continue
			}
		}
		finalOut.WriteByte(raw[i])
	}

	return finalOut.Bytes()
}

func extractImports(val any) []string {
	if val == nil {
		return nil
	}
	arr, ok := val.([]any)
	if !ok {
		return nil
	}
	var res []string
	for _, item := range arr {
		if str, ok := item.(string); ok && str != "" {
			res = append(res, str)
		}
	}
	return res
}

func updateImports(existing []string, scriptURI, legacyStyleURI string) []string {
	var next []string
	scriptURILower := strings.ToLower(scriptURI)
	legacyStyleURILower := strings.ToLower(legacyStyleURI)

	for _, item := range existing {
		lower := strings.ToLower(item)
		if lower != scriptURILower && lower != legacyStyleURILower && lower != strings.TrimPrefix(scriptURILower, "file://") {
			next = append(next, item)
		}
	}
	next = append(next, scriptURI)
	return next
}

func extractFloat(v any, def float64) float64 {
	switch n := v.(type) {
	case float64:
		if n < 0.03 {
			return 0.03
		}
		if n > 1.0 {
			return 1.0
		}
		return n
	case int:
		return float64(n)
	default:
		return def
	}
}

func extractInt(v any, def int) int {
	switch n := v.(type) {
	case float64:
		val := int(n)
		if val < 0 {
			return 0
		}
		if val > 30 {
			return 30
		}
		return val
	case int:
		if n < 0 {
			return 0
		}
		if n > 30 {
			return 30
		}
		return n
	default:
		return def
	}
}

func extractString(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		switch s {
		case "center", "top", "bottom", "left", "right":
			return s
		}
	}
	return def
}

// WorkbenchImageURI transforms a local image file path into a VS Code vscode-file:// URL.
func WorkbenchImageURI(absPath string) string {
	slash := strings.ReplaceAll(absPath, "\\", "/")
	parts := strings.Split(slash, "/")
	var escapedParts []string

	for i, part := range parts {
		if i == 0 && len(part) == 2 && part[1] == ':' {
			// Windows drive letter like C: -> /c%3A
			drive := strings.ToLower(string(part[0]))
			escapedParts = append(escapedParts, "", drive+"%3A")
			continue
		}
		if i == 0 && part == "" {
			escapedParts = append(escapedParts, "")
			continue
		}
		escapedParts = append(escapedParts, url.PathEscape(part))
	}
	return "vscode-file://vscode-app" + strings.Join(escapedParts, "/")
}

// FileURI returns a standard file:/// URI for the given absolute path.
func FileURI(absPath string) string {
	slash := strings.ReplaceAll(absPath, "\\", "/")
	parts := strings.Split(slash, "/")
	var escapedParts []string

	for i, part := range parts {
		if i == 0 && len(part) == 2 && part[1] == ':' {
			drive := strings.ToLower(string(part[0]))
			escapedParts = append(escapedParts, "", drive+"%3A")
			continue
		}
		if i == 0 && part == "" {
			escapedParts = append(escapedParts, "")
			continue
		}
		escapedParts = append(escapedParts, url.PathEscape(part))
	}
	return "file://" + strings.Join(escapedParts, "/")
}

// GenerateStylesheet creates the bggo-wallpaper.css content that VS Code's workbench executes.
func GenerateStylesheet(imageURI string, opacity float64, blur int, position string) string {
	return fmt.Sprintf(`/* Generated by BGGo Wallpaper. */
body::after {
  content: "";
  position: fixed;
  inset: -%dpx;
  z-index: 1001;
  pointer-events: none;
  background-image: url(%q);
  background-position: %s;
  background-repeat: no-repeat;
  background-size: cover;
  opacity: %.2f;
  filter: blur(%dpx);
}
`, blur*2, imageURI, position, opacity, blur)
}

// GenerateScript creates the bggo-wallpaper.js content that VS Code's workbench executes.
func GenerateScript(imageURI string, opacity float64, blur int, position string) string {
	styleMap := map[string]string{
		"position":           "fixed",
		"inset":              fmt.Sprintf("-%dpx", blur*2),
		"zIndex":             "1001",
		"pointerEvents":      "none",
		"backgroundImage":    fmt.Sprintf("url(%q)", imageURI),
		"backgroundPosition": position,
		"backgroundRepeat":   "no-repeat",
		"backgroundSize":     "cover",
		"opacity":            fmt.Sprintf("%.2f", opacity),
		"filter":             fmt.Sprintf("blur(%dpx)", blur),
	}
	styleJSON, _ := json.Marshal(styleMap)

	return fmt.Sprintf(`/* Generated by BGGo Wallpaper. */
(() => {
  const render = () => {
    const id = "bggo-wallpaper-layer";
    const layer = document.getElementById(id) || document.body.appendChild(document.createElement("div"));
    layer.id = id;
    Object.assign(layer.style, %s);
  };
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", render, { once: true });
  } else {
    render();
  }
})();
`, string(styleJSON))
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
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
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
