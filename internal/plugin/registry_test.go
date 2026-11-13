package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogClientVerifiesCatalogAndPackage(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "example.zip")
	writePluginArchive(t, archivePath, "remote-demo", "2.0.0", `export function ok() { return true; }`)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archiveData)
	entry := CatalogEntry{
		Manifest: Manifest{
			Type:       pluginType,
			APIVersion: pluginAPIVersion,
			ID:         "remote-demo",
			Name:       "Remote Demo",
			Version:    "2.0.0",
			Main:       "index.js",
		},
		Size:      int64(len(archiveData)),
		SHA256:    hex.EncodeToString(digest[:]),
		Signature: SignPackageDigest(privateKey, archiveData),
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugin.zip" {
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(archiveData)
			return
		}
		catalog := Catalog{
			Version:     catalogVersion,
			GeneratedAt: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
			Plugins:     []CatalogEntry{entry},
		}
		entry.URL = "https://" + r.Host + "/plugin.zip"
		catalog.Plugins[0] = entry
		payload, err := CatalogPayloadJSON(catalog)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		document := catalogDocument{
			Payload:   catalog,
			Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(document)
	}))
	defer server.Close()

	entry.URL = server.URL + "/plugin.zip"
	client, err := NewCatalogClient(server.URL+"/catalog", base64.StdEncoding.EncodeToString(publicKey), server.Client())
	if err != nil {
		t.Fatalf("NewCatalogClient() failed: %v", err)
	}
	catalog, err := client.Fetch(t.Context())
	if err != nil {
		t.Fatalf("Fetch() failed: %v", err)
	}
	if len(catalog.Plugins) != 1 || catalog.Plugins[0].Manifest.ID != "remote-demo" {
		t.Fatalf("Fetch() catalog = %#v", catalog)
	}
	// The server signs the URL it serves, so use the fetched entry for download.
	entry = catalog.Plugins[0]
	archive, err := client.Download(t.Context(), entry, t.TempDir())
	if err != nil {
		t.Fatalf("Download() failed: %v", err)
	}
	defer os.Remove(archive)
	manifest, err := InspectArchive(archive)
	if err != nil {
		t.Fatalf("InspectArchive(downloaded) failed: %v", err)
	}
	if manifest.ID != "remote-demo" || manifest.Version != "2.0.0" {
		t.Fatalf("downloaded manifest = %#v", manifest)
	}
}

func TestCatalogClientRejectsTamperedPackage(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("signed package")
	digest := sha256.Sum256(data)
	entry := CatalogEntry{
		Manifest:  Manifest{Type: pluginType, APIVersion: pluginAPIVersion, ID: "tamper-demo", Name: "Tamper", Version: "1.0.0", Main: "index.js"},
		Size:      int64(len(data)),
		SHA256:    hex.EncodeToString(digest[:]),
		Signature: SignPackageDigest(privateKey, data),
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tampered package"))
	}))
	defer server.Close()
	entry.URL = server.URL
	client, err := NewCatalogClient(server.URL, base64.StdEncoding.EncodeToString(publicKey), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Download(t.Context(), entry, t.TempDir()); err == nil {
		t.Fatal("Download() accepted a tampered package")
	}
}

func TestCompareVersions(t *testing.T) {
	if CompareVersions("v1.2.0", "1.1.9") <= 0 {
		t.Fatal("newer plugin version was not greater")
	}
	if CompareVersions("1.0.0", "1.0.0") != 0 {
		t.Fatal("equal plugin versions differed")
	}
	if CompareVersions("dev", "stable") >= 0 {
		t.Fatal("fallback version comparison was unexpected")
	}
}
