package vscode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkbenchImageURI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Linux path with chinese",
			input:    "/home/ubuntu/图片/bing-wallpaper/th.jpeg",
			expected: "vscode-file://vscode-app/home/ubuntu/%E5%9B%BE%E7%89%87/bing-wallpaper/th.jpeg",
		},
		{
			name:     "Linux path with spaces",
			input:    "/home/ubuntu/My Pictures/bg.png",
			expected: "vscode-file://vscode-app/home/ubuntu/My%20Pictures/bg.png",
		},
		{
			name:     "Windows style path",
			input:    "C:\\Users\\alice\\Pictures\\wall.jpg",
			expected: "vscode-file://vscode-app/c%3A/Users/alice/Pictures/wall.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WorkbenchImageURI(tt.input)
			if got != tt.expected {
				t.Errorf("WorkbenchImageURI(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFileURI(t *testing.T) {
	uri := FileURI("/home/ubuntu/.config/Code/User/globalStorage/bggo.bggo-wallpaper/bggo-wallpaper.js")
	expected := "file:///home/ubuntu/.config/Code/User/globalStorage/bggo.bggo-wallpaper/bggo-wallpaper.js"
	if uri != expected {
		t.Errorf("FileURI() = %q, want %q", uri, expected)
	}
}

func TestCleanJSONC(t *testing.T) {
	input := `{
		// This is a line comment
		"key": "value // not a comment",
		/* This is a
		   block comment */
		"number": 42,
		"arr": [1, 2, ],
	}`
	cleaned := cleanJSONC([]byte(input))
	var m map[string]any
	if err := json.Unmarshal(cleaned, &m); err != nil {
		t.Fatalf("cleanJSONC failed to produce valid JSON: %v\nCleaned output:\n%s", err, string(cleaned))
	}
	if m["key"] != "value // not a comment" {
		t.Errorf("expected string content preserved, got %v", m["key"])
	}
	if m["number"] != float64(42) {
		t.Errorf("expected 42, got %v", m["number"])
	}
}

func TestApplyWallpaper(t *testing.T) {
	tmpDir := t.TempDir()
	userDir := filepath.Join(tmpDir, "User")
	extDir := filepath.Join(tmpDir, "extensions")
	_ = os.MkdirAll(userDir, 0o755)
	_ = os.MkdirAll(filepath.Join(extDir, "bggo.bggo-wallpaper-0.1.5"), 0o755)

	// Create existing settings.json with prior settings and comments
	initialSettings := `{
		// My personal comment
		"editor.fontSize": 14,
		"vscode_custom_css.imports": [
			"file:///some/other/style.css"
		],
	}`
	_ = os.WriteFile(filepath.Join(userDir, "settings.json"), []byte(initialSettings), 0o644)

	// Create a dummy image file
	dummyImg := filepath.Join(tmpDir, "sample.jpg")
	_ = os.WriteFile(dummyImg, []byte("fake-image-data"), 0o644)

	svc := Service{
		UserDir:      userDir,
		ExtensionDir: extDir,
	}

	report, err := svc.Apply(dummyImg)
	if err != nil {
		t.Fatalf("svc.Apply failed: %v", err)
	}

	if !report.ExtensionInstalled {
		t.Errorf("expected ExtensionInstalled=true")
	}

	// Verify settings.json
	settingsData, err := os.ReadFile(filepath.Join(userDir, "settings.json"))
	if err != nil {
		t.Fatalf("failed to read settings.json: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatalf("invalid json in settings.json: %v", err)
	}

	if settings[SettingWallpaperPath] != dummyImg {
		t.Errorf("SettingWallpaperPath = %v, want %v", settings[SettingWallpaperPath], dummyImg)
	}
	if settings["editor.fontSize"] != float64(14) {
		t.Errorf("editor.fontSize = %v, want 14", settings["editor.fontSize"])
	}

	imports, ok := settings[SettingCustomCSSImports].([]any)
	if !ok || len(imports) != 2 {
		t.Fatalf("expected 2 items in imports, got %v", settings[SettingCustomCSSImports])
	}
	if imports[0] != "file:///some/other/style.css" {
		t.Errorf("imports[0] = %v, want file:///some/other/style.css", imports[0])
	}
	expectedScriptURI := FileURI(filepath.Join(userDir, "globalStorage", ManagedExtensionID, ManagedScriptFile))
	if imports[1] != expectedScriptURI {
		t.Errorf("imports[1] = %v, want %v", imports[1], expectedScriptURI)
	}

	// Verify bggo-wallpaper.js
	scriptData, err := os.ReadFile(report.ScriptPath)
	if err != nil {
		t.Fatalf("failed to read script: %v", err)
	}
	scriptStr := string(scriptData)
	if !strings.Contains(scriptStr, "bggo-wallpaper-layer") {
		t.Errorf("script missing bggo-wallpaper-layer: %s", scriptStr)
	}
	if !strings.Contains(scriptStr, WorkbenchImageURI(dummyImg)) {
		t.Errorf("script missing image URI: %s", scriptStr)
	}
}

func TestApplyRejectsInvalidFile(t *testing.T) {
	tmpDir := t.TempDir()
	svc := Service{UserDir: tmpDir}

	// Directory
	_, err := svc.Apply(tmpDir)
	if err == nil || !strings.Contains(err.Error(), "目录") {
		t.Errorf("expected directory error, got %v", err)
	}

	// Unsupported extension
	txtFile := filepath.Join(tmpDir, "text.txt")
	_ = os.WriteFile(txtFile, []byte("hello"), 0o644)
	_, err = svc.Apply(txtFile)
	if err == nil || !strings.Contains(err.Error(), "不支持") {
		t.Errorf("expected unsupported extension error, got %v", err)
	}
}
