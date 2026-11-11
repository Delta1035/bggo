package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
}

// CheckResult contains the result of checking for updates.
type CheckResult struct {
	HasUpdate     bool
	LatestVersion string
	Manifest      *Manifest
}

// Config configures the updater service.
type Config struct {
	Repo       string // e.g. "Delta1035/bggo"
	TagPrefix  string // e.g. "desktop-v"
	PublicKey  string // Ed25519 base64
	HTTPClient *http.Client
	APIBaseURL string // default: "https://api.github.com"
	AuthToken  string
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
	return &Service{cfg: cfg}
}

// FindLatestRelease finds the latest non-draft, non-prerelease tag starting with tagPrefix.
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
		if !r.Draft && !r.Prerelease && strings.HasPrefix(r.TagName, s.cfg.TagPrefix) {
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
func (s *Service) CheckUpdate(ctx context.Context, currentVersion, target string) (*CheckResult, error) {
	tag, err := s.FindLatestRelease(ctx)
	if err != nil {
		return nil, err
	}

	manifest, err := s.FetchManifest(ctx, tag, target)
	if err != nil {
		return nil, err
	}

	hasUpdate := CompareVersions(manifest.Version, currentVersion) > 0
	return &CheckResult{
		HasUpdate:     hasUpdate,
		LatestVersion: manifest.Version,
		Manifest:      manifest,
	}, nil
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
