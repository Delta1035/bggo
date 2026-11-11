package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bggo/internal/library"
	"bggo/internal/vscode"
	"github.com/egoist/mygo/ui"
)

func TestInitialView(t *testing.T) {
	a := &application{fit: "覆盖", status: "选择一张图片开始"}
	tt := ui.NewTester(a.view, 1000, 680)
	for _, text := range []string{
		"BGGo",
		AppVersion,
		"桌面壁纸",
		"还没有选择壁纸",
		"应用到桌面",
		"映射到 VS Code",
		"同步到 VS Code",
		"检查更新",
	} {
		if !tt.HasText(text) {
			t.Errorf("missing text %q; texts = %q", text, tt.Texts())
		}
	}
	if tt.HasText("Linux 优先 · Windows 已适配") {
		t.Errorf("expected 'Linux 优先 · Windows 已适配' to be removed")
	}
}

func TestPreviewFit(t *testing.T) {
	tests := map[string]ui.Fit{
		"覆盖": ui.Cover,
		"适应": ui.Contain,
		"拉伸": ui.FillBox,
	}
	for name, want := range tests {
		if got := previewFit(name); got != want {
			t.Errorf("previewFit(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestToggleSyncVSCode(t *testing.T) {
	tmp := t.TempDir()
	store := &library.Store{}
	// Use library package reflection or store in temp dir by creating store file
	storePath := filepath.Join(tmp, "library.json")
	storeRef := store
	// store has unexported path, we can test via SetSyncVSCode directly on library.Store or initialize application
	_ = storePath
	_ = storeRef

	a := &application{syncVSCode: false}
	a.syncVSCode = true
	a.toggleSyncVSCode()
	if !a.syncVSCode {
		t.Errorf("expected syncVSCode=true")
	}
}

func TestApplyVSCodeEmpty(t *testing.T) {
	a := &application{selected: ""}
	a.applyVSCode()
	if a.busy {
		t.Errorf("expected not busy when selected is empty")
	}
}

func TestApplyVSCodeService(t *testing.T) {
	tmp := t.TempDir()
	userDir := filepath.Join(tmp, "User")
	dummyImg := filepath.Join(tmp, "wall.png")

	a := &application{
		selected: dummyImg,
		vscode:   vscode.Service{UserDir: userDir},
	}
	// Calling applyVSCode should handle background execution without crashing when window is nil
	a.applyVSCode()
}

func TestCheckForUpdates(t *testing.T) {
	called := false
	a := &application{
		checkUpdate: func() {
			called = true
		},
	}
	a.checkForUpdates()
	if !called {
		t.Errorf("expected checkUpdate callback to be called")
	}
	if a.status != "正在检查应用更新…" {
		t.Errorf("status = %q, want %q", a.status, "正在检查应用更新…")
	}
	if a.error {
		t.Errorf("expected error=false")
	}
}

func TestApplyVSCode_AutoInstallExtension(t *testing.T) {
	tmp := t.TempDir()
	userDir := filepath.Join(tmp, "User")
	dummyImg := filepath.Join(tmp, "wall.png")
	_ = os.WriteFile(dummyImg, []byte("pngdata"), 0o644)

	installedDone := make(chan struct{})
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		close(installedDone)
		return []byte("installed"), nil
	}

	fakeCLI := filepath.Join(tmp, "code")
	_ = os.WriteFile(fakeCLI, []byte(""), 0o755)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fake-vsix"))
	}))
	defer srv.Close()

	a := &application{
		selected: dummyImg,
		vscode: vscode.Service{
			UserDir:       userDir,
			ExtensionDir:  filepath.Join(tmp, "extensions"),
			VSCodeCLIPath: fakeCLI,
			DownloadURL:   srv.URL,
			CommandRunner: mockRunner,
		},
	}

	a.applyVSCode()

	select {
	case <-installedDone:
		// 成功触发自动下载安装
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for auto-install of extension")
	}
}
