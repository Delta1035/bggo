package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"0.2.1", "0.2.0", 1},
		{"v0.2.1", "0.2.1", 0},
		{"0.2.0", "0.2.1", -1},
		{"1.0.0", "0.9.9", 1},
		{"0.1.0", "0.1.0", 0},
		{"0.2.10", "0.2.9", 1},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_vs_%s", tt.a, tt.b), func(t *testing.T) {
			got := CompareVersions(tt.a, tt.b)
			if got != tt.expected {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestService_CheckUpdate_MockServer(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pubBase64 := base64.StdEncoding.EncodeToString(pub)

	fakeArchiveData := []byte("fake-bggo-archive-payload")
	digest := sha256.Sum256(fakeArchiveData)
	sig := ed25519.Sign(priv, digest[:])
	sigBase64 := base64.StdEncoding.EncodeToString(sig)

	mux := http.NewServeMux()

	// Mock GitHub releases API: includes prereleases and drafts to ensure filtering
	mux.HandleFunc("/repos/testowner/testrepo/releases", func(w http.ResponseWriter, r *http.Request) {
		releases := []Release{
			{TagName: "desktop-v0.3.0-beta", Draft: false, Prerelease: true},
			{TagName: "desktop-v0.2.2", Draft: true, Prerelease: false},
			{TagName: "desktop-v0.2.1", Draft: false, Prerelease: false},
			{TagName: "desktop-v0.1.0", Draft: false, Prerelease: false},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(releases)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	svc := NewService(Config{
		Repo:       "testowner/testrepo",
		TagPrefix:  "desktop-v",
		PublicKey:  pubBase64,
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	})

	// 1. Verify FindLatestRelease filters draft and prerelease correctly
	tag, err := svc.FindLatestRelease(context.Background())
	if err != nil {
		t.Fatalf("FindLatestRelease failed: %v", err)
	}
	if tag != "desktop-v0.2.1" {
		t.Errorf("expected latest tag desktop-v0.2.1, got %s", tag)
	}

	// 2. Verify archive signature verification
	if err := svc.VerifyArchive(fakeArchiveData, sigBase64); err != nil {
		t.Errorf("VerifyArchive failed: %v", err)
	}

	// Corrupted archive should fail verification
	if err := svc.VerifyArchive([]byte("corrupted"), sigBase64); err == nil {
		t.Errorf("VerifyArchive should fail on corrupted data")
	}
}

func TestFetchLatestManifestAndCheckUpdate(t *testing.T) {
	manifestPayload := Manifest{
		Version:   "0.3.0",
		Date:      "2026-11-12T00:00:00Z",
		URL:       "https://example.com/bggo-0.3.0-linux-amd64.tar.gz",
		Size:      12345,
		Signature: "fake-sig",
		Notes:     "Feature updates",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/Delta1035/bggo/releases/latest/download/update-linux-amd64.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifestPayload)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	svc := NewService(Config{
		Repo:          "Delta1035/bggo",
		TagPrefix:     "v",
		PublicKey:     "Q6BKxOcOtIoRKdZIpkmUZzK6SePGA7EUGjIxgdjoIQ4=",
		HTTPClient:    server.Client(),
		UpdateFeedURL: server.URL + "/Delta1035/bggo/releases/latest/download/update-linux-amd64.json",
	})

	// 1. FetchLatestManifest
	manifest, err := svc.FetchLatestManifest(context.Background(), "linux-amd64")
	if err != nil {
		t.Fatalf("FetchLatestManifest failed: %v", err)
	}
	if manifest.Version != "0.3.0" {
		t.Errorf("expected version 0.3.0, got %s", manifest.Version)
	}

	// 2. CheckUpdate when newer version available
	resNew, err := svc.CheckUpdate(context.Background(), "0.2.6", "linux-amd64")
	if err != nil {
		t.Fatalf("CheckUpdate with older current version failed: %v", err)
	}
	if !resNew.HasUpdate {
		t.Errorf("expected HasUpdate=true for 0.3.0 vs 0.2.6")
	}
	if resNew.LatestVersion != "0.3.0" {
		t.Errorf("expected LatestVersion=0.3.0, got %s", resNew.LatestVersion)
	}

	// 3. CheckUpdate when already on latest version
	resSame, err := svc.CheckUpdate(context.Background(), "0.3.0", "linux-amd64")
	if err != nil {
		t.Fatalf("CheckUpdate with same version failed: %v", err)
	}
	if resSame.HasUpdate {
		t.Errorf("expected HasUpdate=false for 0.3.0 vs 0.3.0")
	}

	// 4. CheckUpdate when running newer dev version
	resNewer, err := svc.CheckUpdate(context.Background(), "0.3.1", "linux-amd64")
	if err != nil {
		t.Fatalf("CheckUpdate with newer current version failed: %v", err)
	}
	if resNewer.HasUpdate {
		t.Errorf("expected HasUpdate=false for 0.3.0 vs 0.3.1")
	}
}

func TestDownloadFile(t *testing.T) {
	fileContent := "test package content bytes 1234567890"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fileContent)))
		_, _ = w.Write([]byte(fileContent))
	}))
	defer server.Close()

	svc := NewService(Config{
		HTTPClient: server.Client(),
	})

	tmpDir := t.TempDir()
	dest := tmpDir + "/test_deb.deb"

	var progressCalled bool
	var lastDownloaded int64
	err := svc.DownloadFile(context.Background(), server.URL, dest, func(downloaded, total int64) {
		progressCalled = true
		lastDownloaded = downloaded
	})
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}

	if !progressCalled {
		t.Errorf("expected progress callback to be called")
	}
	if lastDownloaded != int64(len(fileContent)) {
		t.Errorf("downloaded = %d, want %d", lastDownloaded, len(fileContent))
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != fileContent {
		t.Errorf("downloaded content mismatch, got %q", string(data))
	}
}

func TestGetDebDownloadURL(t *testing.T) {
	svc := NewService(Config{
		Repo: "Delta1035/bggo",
	})
	url1 := svc.GetDebDownloadURL("0.2.6")
	expected1 := "https://github.com/Delta1035/bggo/releases/download/v0.2.6/bggo_0.2.6_amd64.deb"
	if url1 != expected1 {
		t.Errorf("GetDebDownloadURL(0.2.6) = %q, want %q", url1, expected1)
	}

	url2 := svc.GetDebDownloadURL("v0.3.0")
	expected2 := "https://github.com/Delta1035/bggo/releases/download/v0.3.0/bggo_0.3.0_amd64.deb"
	if url2 != expected2 {
		t.Errorf("GetDebDownloadURL(v0.3.0) = %q, want %q", url2, expected2)
	}
}

func TestInstallDebPackage_InvalidFile(t *testing.T) {
	svc := NewService(Config{})
	err := svc.InstallDebPackage("/non/existent/path/bggo.deb")
	if err == nil {
		t.Errorf("expected error for non-existent deb package")
	}
}

func TestInstallDebPackage_GraphicalInstallerIsPending(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("xdg-open fallback is Linux-specific")
	}

	binDir := t.TempDir()
	openPath := filepath.Join(binDir, "xdg-open")
	if err := os.WriteFile(openPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	debPath := filepath.Join(t.TempDir(), "bggo.deb")
	if err := os.WriteFile(debPath, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := NewService(Config{}).InstallDebPackage(debPath)
	if !errors.Is(err, ErrInstallPending) {
		t.Fatalf("InstallDebPackage() error = %v, want ErrInstallPending", err)
	}
}

func TestLiveGitHubUpdateDetection(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	svc := NewService(Config{
		Repo:      "Delta1035/bggo",
		TagPrefix: "v",
		PublicKey: "Q6BKxOcOtIoRKdZIpkmUZzK6SePGA7EUGjIxgdjoIQ4=",
		AuthToken: token,
	})

	tag, err := svc.FindLatestRelease(ctx)
	if err != nil {
		// If hit GitHub rate limit in non-auth CI, skip rather than fail
		t.Skipf("skipping live GitHub check: %v", err)
		return
	}

	if !strings.HasPrefix(tag, "v") {
		t.Errorf("expected live latest tag to start with v, got %q", tag)
	}

	manifest, err := svc.FetchManifest(ctx, tag, "linux-amd64")
	if err != nil {
		t.Skipf("skipping live manifest download due to network: %v", err)
		return
	}

	if manifest.Version == "" || manifest.Signature == "" {
		t.Errorf("manifest incomplete: %+v", manifest)
	}

	t.Logf("Live update resolved: tag=%s, version=%s, size=%d", tag, manifest.Version, manifest.Size)
}
