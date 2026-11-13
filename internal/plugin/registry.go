package plugin

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	maxCatalogBytes        = 2 << 20
	maxPluginPackageBytes  = 32 << 20
	catalogVersion         = 1
	pluginCatalogUserAgent = "BGGo-Plugin-Catalog/1"
)

// Catalog is the signed list of plugins published by a trusted maintainer.
// The catalog contains metadata only; executable code is downloaded separately
// and verified against the entry before the archive installer sees it.
type Catalog struct {
	Version     int            `json:"version"`
	GeneratedAt string         `json:"generatedAt"`
	Plugins     []CatalogEntry `json:"plugins"`
}

// CatalogEntry describes one signed plugin package.
type CatalogEntry struct {
	Manifest  Manifest `json:"manifest"`
	URL       string   `json:"url"`
	Size      int64    `json:"size"`
	SHA256    string   `json:"sha256"`
	Signature string   `json:"signature"`
}

type catalogDocument struct {
	Payload   Catalog `json:"payload"`
	Signature string  `json:"signature"`
}

// CatalogClient fetches signed catalogs and downloads signed plugin packages.
type CatalogClient struct {
	url        string
	publicKey  ed25519.PublicKey
	httpClient *http.Client
}

// NewCatalogClient creates a client for an HTTPS catalog and its Ed25519 key.
// The same public key authenticates the catalog and each package digest.
func NewCatalogClient(catalogURL, publicKeyBase64 string, httpClient *http.Client) (*CatalogClient, error) {
	if err := validateHTTPSURL(catalogURL); err != nil {
		return nil, fmt.Errorf("插件目录地址无效: %w", err)
	}
	publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyBase64))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return nil, errors.New("插件目录公钥无效")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &CatalogClient{
		url:        catalogURL,
		publicKey:  append(ed25519.PublicKey(nil), publicKey...),
		httpClient: httpClient,
	}, nil
}

// Fetch verifies and decodes the signed catalog.
func (c *CatalogClient) Fetch(ctx context.Context) (Catalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return Catalog{}, fmt.Errorf("创建插件目录请求失败: %w", err)
	}
	req.Header.Set("User-Agent", pluginCatalogUserAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Catalog{}, fmt.Errorf("下载插件目录失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Catalog{}, fmt.Errorf("插件目录返回异常状态码: %d", resp.StatusCode)
	}
	if resp.ContentLength > maxCatalogBytes {
		return Catalog{}, errors.New("插件目录过大")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil {
		return Catalog{}, fmt.Errorf("读取插件目录失败: %w", err)
	}
	if len(data) > maxCatalogBytes {
		return Catalog{}, errors.New("插件目录过大")
	}
	var document catalogDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return Catalog{}, fmt.Errorf("解析插件目录失败: %w", err)
	}
	signature, err := base64.StdEncoding.DecodeString(document.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return Catalog{}, errors.New("插件目录签名无效")
	}
	payload, err := json.Marshal(document.Payload)
	if err != nil {
		return Catalog{}, fmt.Errorf("编码插件目录签名内容失败: %w", err)
	}
	if !ed25519.Verify(c.publicKey, payload, signature) {
		return Catalog{}, errors.New("插件目录签名校验失败")
	}
	if err := validateCatalog(document.Payload); err != nil {
		return Catalog{}, err
	}
	return document.Payload, nil
}

// Download verifies one package and returns a temporary archive path. The
// caller owns the returned file and should remove it after installation.
func (c *CatalogClient) Download(ctx context.Context, entry CatalogEntry, destination string) (string, error) {
	if err := validateCatalogEntry(entry); err != nil {
		return "", err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return "", fmt.Errorf("创建插件下载目录失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.URL, nil)
	if err != nil {
		return "", fmt.Errorf("创建插件下载请求失败: %w", err)
	}
	req.Header.Set("User-Agent", pluginCatalogUserAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载插件包失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("插件包返回异常状态码: %d", resp.StatusCode)
	}
	if resp.ContentLength > maxPluginPackageBytes || resp.ContentLength > 0 && resp.ContentLength != entry.Size {
		return "", errors.New("插件包大小与目录记录不一致")
	}
	tmp, err := os.CreateTemp(destination, ".plugin-download-*.zip")
	if err != nil {
		return "", fmt.Errorf("创建插件临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		_ = tmp.Close()
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()

	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, digest), io.LimitReader(resp.Body, maxPluginPackageBytes+1))
	if err != nil {
		return "", fmt.Errorf("读取插件包失败: %w", err)
	}
	if written > maxPluginPackageBytes || written != entry.Size {
		return "", errors.New("插件包大小与目录记录不一致")
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("关闭插件临时文件失败: %w", err)
	}
	digestBytes := digest.Sum(nil)
	if !strings.EqualFold(hex.EncodeToString(digestBytes), entry.SHA256) {
		return "", errors.New("插件包 SHA-256 校验失败")
	}
	signature, err := base64.StdEncoding.DecodeString(entry.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(c.publicKey, digestBytes, signature) {
		return "", errors.New("插件包签名校验失败")
	}
	manifest, err := InspectArchive(tmpPath)
	if err != nil {
		return "", fmt.Errorf("远程插件包校验失败: %w", err)
	}
	if manifest.ID != entry.Manifest.ID || manifest.Version != entry.Manifest.Version {
		return "", errors.New("插件包 manifest 与目录记录不一致")
	}
	removeTemp = false
	return tmpPath, nil
}

func validateCatalog(catalog Catalog) error {
	if catalog.Version != catalogVersion {
		return fmt.Errorf("不支持的插件目录版本: %d", catalog.Version)
	}
	if _, err := time.Parse(time.RFC3339, catalog.GeneratedAt); err != nil {
		return fmt.Errorf("插件目录生成时间无效: %w", err)
	}
	seen := make(map[string]struct{}, len(catalog.Plugins))
	for _, entry := range catalog.Plugins {
		if err := validateCatalogEntry(entry); err != nil {
			return err
		}
		if _, ok := seen[entry.Manifest.ID]; ok {
			return fmt.Errorf("插件目录包含重复 ID: %q", entry.Manifest.ID)
		}
		seen[entry.Manifest.ID] = struct{}{}
	}
	return nil
}

func validateCatalogEntry(entry CatalogEntry) error {
	if err := validateManifest(&entry.Manifest); err != nil {
		return fmt.Errorf("插件目录 manifest 无效: %w", err)
	}
	if err := validateHTTPSURL(entry.URL); err != nil {
		return fmt.Errorf("插件下载地址无效: %w", err)
	}
	if entry.Size <= 0 || entry.Size > maxPluginPackageBytes {
		return errors.New("插件包大小无效")
	}
	digest, err := hex.DecodeString(entry.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return errors.New("插件包 SHA-256 无效")
	}
	signature, err := base64.StdEncoding.DecodeString(entry.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("插件包签名无效")
	}
	return nil
}

func validateHTTPSURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return errors.New("必须使用不带用户信息和片段的 HTTPS 地址")
	}
	return nil
}

// CompareVersions compares the numeric parts of two BGGo plugin versions.
func CompareVersions(left, right string) int {
	l, leftErr := versionParts(left)
	r, rightErr := versionParts(right)
	if leftErr != nil || rightErr != nil {
		return strings.Compare(strings.TrimPrefix(left, "v"), strings.TrimPrefix(right, "v"))
	}
	for i := range l {
		if l[i] > r[i] {
			return 1
		}
		if l[i] < r[i] {
			return -1
		}
	}
	return 0
}

// SignPackageDigest returns the package signature format used by catalogs.
// It is provided for release tooling and tests; private keys stay outside the app.
func SignPackageDigest(privateKey ed25519.PrivateKey, data []byte) string {
	digest := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, digest[:]))
}

// CatalogPayloadJSON returns the canonical bytes that must be signed for a catalog.
func CatalogPayloadJSON(catalog Catalog) ([]byte, error) {
	return json.Marshal(catalog)
}
