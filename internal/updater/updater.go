package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Release represents a GitHub release entry.
type Release struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// Manifest represents the update manifest metadata JSON.
type Manifest struct {
	Version   string `json:"version"`
	Date      string `json:"date"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	Signature string `json:"signature"`
	Notes     string `json:"notes,omitempty"`
}

// CheckResult contains the result of checking for updates.
type CheckResult struct {
	HasUpdate     bool
	LatestVersion string
	Manifest      *Manifest
}

// Config configures the updater service.
type Config struct {
	Repo          string // e.g. "Delta1035/bggo"
	TagPrefix     string // e.g. "v"
	PublicKey     string // Ed25519 base64
	HTTPClient    *http.Client
	APIBaseURL    string // default: "https://api.github.com"
	AuthToken     string
	UpdateFeedURL string // optional override for direct manifest URL
}

// Service provides update discovery and validation methods.
type Service struct {
	cfg Config
}

// NewService creates a new updater service.
func NewService(cfg Config) *Service {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = "https://api.github.com"
	}
	if cfg.TagPrefix == "" {
		cfg.TagPrefix = "v"
	}
	return &Service{cfg: cfg}
}

// Repo returns the configured repository string.
func (s *Service) Repo() string {
	return s.cfg.Repo
}

// FetchLatestManifest fetches update-<target>.json from the latest release asset download URL.
// This direct download avoids GitHub API rate limits.
func (s *Service) FetchLatestManifest(ctx context.Context, target string) (*Manifest, error) {
	manifestURL := s.cfg.UpdateFeedURL
	if manifestURL == "" {
		manifestURL = fmt.Sprintf("https://github.com/%s/releases/latest/download/update-%s.json",
			s.cfg.Repo, url.PathEscape(target))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BGGo-Desktop-Updater")
	if s.cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.AuthToken)
	}

	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载最新更新清单: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载最新更新清单失败，状态码 %d", resp.StatusCode)
	}

	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("解析更新清单: %w", err)
	}

	if m.Version == "" || m.URL == "" || m.Signature == "" {
		return nil, errors.New("更新清单信息不完整")
	}

	return &m, nil
}

// FindLatestRelease finds the latest non-draft, non-prerelease tag starting with tagPrefix via GitHub API.
func (s *Service) FindLatestRelease(ctx context.Context) (string, error) {
	u := fmt.Sprintf("%s/repos/%s/releases?per_page=100", s.cfg.APIBaseURL, s.cfg.Repo)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "BGGo-Desktop-Updater")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if s.cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.AuthToken)
	}

	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 GitHub Releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API 返回状态码 %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("解析 Releases 列表: %w", err)
	}

	for _, r := range releases {
		if r.Draft || r.Prerelease {
			continue
		}
		if s.cfg.TagPrefix == "v" && strings.HasPrefix(r.TagName, "vscode-") {
			continue
		}
		if strings.HasPrefix(r.TagName, s.cfg.TagPrefix) {
			return r.TagName, nil
		}
	}

	return "", errors.New("未找到可用的正式发布版本")
}

// FetchManifest downloads the update manifest for the specified tag and target.
func (s *Service) FetchManifest(ctx context.Context, tag, target string) (*Manifest, error) {
	manifestURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/update-%s.json",
		s.cfg.Repo, url.PathEscape(tag), url.PathEscape(target))
	req, err := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BGGo-Desktop-Updater")
	if s.cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.AuthToken)
	}

	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载更新清单: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载更新清单失败，状态码 %d", resp.StatusCode)
	}

	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("解析更新清单: %w", err)
	}

	if m.Version == "" || m.URL == "" || m.Signature == "" {
		return nil, errors.New("更新清单信息不完整")
	}

	return &m, nil
}

// CheckUpdate checks if an update is available comparing with currentVersion.
// It prioritizes direct manifest download (no rate-limit) and falls back to API search.
func (s *Service) CheckUpdate(ctx context.Context, currentVersion, target string) (*CheckResult, error) {
	manifest, err := s.FetchLatestManifest(ctx, target)
	if err != nil {
		// Fallback to releases API
		tag, tagErr := s.FindLatestRelease(ctx)
		if tagErr != nil {
			return nil, fmt.Errorf("检查更新失败: %w (fallback: %v)", err, tagErr)
		}
		manifest, err = s.FetchManifest(ctx, tag, target)
		if err != nil {
			return nil, err
		}
	}

	hasUpdate := CompareVersions(manifest.Version, currentVersion) > 0
	return &CheckResult{
		HasUpdate:     hasUpdate,
		LatestVersion: manifest.Version,
		Manifest:      manifest,
	}, nil
}

// IsWritableInstall checks if the running app directory is writable by the current user.
func (s *Service) IsWritableInstall() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dir := filepath.Dir(exe)
	probe, err := os.MkdirTemp(dir, ".bggo-update-probe-")
	if err != nil {
		return false
	}
	_ = os.Remove(probe)
	return true
}

// GetDebDownloadURL returns the download URL for the Debian/Ubuntu package for the given version.
func (s *Service) GetDebDownloadURL(version string) string {
	version = strings.TrimPrefix(version, "v")
	return fmt.Sprintf("https://github.com/%s/releases/download/v%s/bggo_%s_amd64.deb",
		s.cfg.Repo, version, version)
}

// DownloadFile downloads a remote file to destPath with optional progress reporting.
func (s *Service) DownloadFile(ctx context.Context, fileURL, destPath string, progress func(downloaded, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "BGGo-Desktop-Updater")
	if s.cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.AuthToken)
	}

	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载文件失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载文件 HTTP 状态码异常: %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("创建存储目录失败: %w", err)
	}

	tmpFile := destPath + ".part"
	out, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer func() {
		_ = out.Close()
		_ = os.Remove(tmpFile)
	}()

	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	for {
		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := out.Write(buf[:n]); wErr != nil {
				return fmt.Errorf("写入临时文件失败: %w", wErr)
			}
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, total)
			}
		}
		if rErr != nil {
			if rErr == io.EOF {
				break
			}
			return fmt.Errorf("读取下载流失败: %w", rErr)
		}
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("关闭文件流失败: %w", err)
	}

	if err := os.Rename(tmpFile, destPath); err != nil {
		return fmt.Errorf("重命名更新包文件失败: %w", err)
	}

	return nil
}

// ErrInstallPending means a graphical installer was opened, but its completion
// cannot be observed by the application.
var ErrInstallPending = errors.New("图形安装器已打开，安装状态待确认")

// InstallDebPackage installs a deb package using pkexec or falls back to xdg-open.
func (s *Service) InstallDebPackage(debPath string) error {
	if _, err := os.Stat(debPath); err != nil {
		return fmt.Errorf("更新包不存在: %w", err)
	}

	// Try pkexec dpkg -i
	pkexec, err := exec.LookPath("pkexec")
	if err == nil && pkexec != "" {
		cmd := exec.Command(pkexec, "dpkg", "-i", debPath)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		log.Printf("pkexec dpkg 安装返回错误: %v, 输出: %s", err, string(out))
	}

	// Fallback to xdg-open for GUI installer
	if xdg, err := exec.LookPath("xdg-open"); err == nil && xdg != "" {
		if err := exec.Command(xdg, debPath).Start(); err == nil {
			return fmt.Errorf("%w: %s", ErrInstallPending, debPath)
		}
	}

	return errors.New("无法启动提权安装或图形安装器，请手动执行: sudo dpkg -i " + debPath)
}

// OpenBrowser opens the specified URL in the system browser.
func (s *Service) OpenBrowser(targetURL string) error {
	switch runtime.GOOS {
	case "linux":
		return exec.Command("xdg-open", targetURL).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL).Start()
	case "darwin":
		return exec.Command("open", targetURL).Start()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// VerifyArchive checks if an archive's SHA256 digest matches the signature in manifest using configured public key.
func (s *Service) VerifyArchive(archiveData []byte, signatureBase64 string) error {
	pubKeyBytes, err := base64.StdEncoding.DecodeString(s.cfg.PublicKey)
	if err != nil {
		return fmt.Errorf("解码公钥: %w", err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return errors.New("无效的 Ed25519 公钥长度")
	}

	sigBytes, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return fmt.Errorf("解码签名: %w", err)
	}
	if len(sigBytes) != ed25519.SignatureSize {
		return errors.New("无效的 Ed25519 签名长度")
	}

	digest := sha256.Sum256(archiveData)
	if !ed25519.Verify(ed25519.PublicKey(pubKeyBytes), digest[:], sigBytes) {
		return errors.New("更新签名校验未通过")
	}
	return nil
}

// CompareVersions compares two semver-like version strings.
// Returns 1 if a > b, -1 if a < b, 0 if equal.
func CompareVersions(a, b string) int {
	a = strings.TrimPrefix(strings.TrimSpace(a), "v")
	b = strings.TrimPrefix(strings.TrimSpace(b), "v")

	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}

	for i := 0; i < maxLen; i++ {
		var numA, numB int
		if i < len(partsA) {
			numA, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			numB, _ = strconv.Atoi(partsB[i])
		}
		if numA > numB {
			return 1
		}
		if numA < numB {
			return -1
		}
	}
	return 0
}
