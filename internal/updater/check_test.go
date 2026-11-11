package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestLiveGitHubUpdateDetection(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	svc := NewService(Config{
		Repo:      "Delta1035/bggo",
		TagPrefix: "desktop-v",
		PublicKey: "Q6BKxOcOtIoRKdZIpkmUZzK6SePGA7EUGjIxgdjoIQ4=",
		AuthToken: token,
	})

	tag, err := svc.FindLatestRelease(ctx)
	if err != nil {
		// If hit GitHub rate limit in non-auth CI, skip rather than fail
		t.Skipf("skipping live GitHub check: %v", err)
		return
	}

	if !strings.HasPrefix(tag, "desktop-v") {
		t.Errorf("expected live latest tag to start with desktop-v, got %q", tag)
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
