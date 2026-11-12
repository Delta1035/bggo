package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"bggo/internal/online"
	"bggo/internal/plugin"
)

// The embedded sources use the same loader as user-installed plugins. This
// keeps the built-in and future external plugin paths behaviorally aligned.
var (
	//go:embed resources/plugins/bing/plugin.json
	builtinBingManifest []byte
	//go:embed resources/plugins/bing/index.js
	builtinBingSource []byte
	//go:embed resources/plugins/qihoo/plugin.json
	builtinQihooManifest []byte
	//go:embed resources/plugins/qihoo/index.js
	builtinQihooSource []byte
)

type embeddedPlugin struct {
	manifest []byte
	filename string
	source   []byte
}

// loadBuiltinWallpaperPlugins replaces the compatibility Go providers only
// after an embedded plugin has compiled successfully. A failed plugin leaves
// its original provider in place so startup and existing users remain usable.
func loadBuiltinWallpaperPlugins(service *online.Service) []*plugin.Plugin {
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Printf("无法确定壁纸插件数据目录，使用 Go 壁纸源: %v", err)
		return nil
	}
	return loadBuiltinWallpaperPluginsAt(service, filepath.Join(configDir, "bggo", "plugin-data"))
}

func loadBuiltinWallpaperPluginsAt(service *online.Service, dataDir string) []*plugin.Plugin {
	definitions := builtinPluginDefinitions()
	loaded := make([]*plugin.Plugin, 0, len(definitions))
	for _, definition := range definitions {
		var manifest plugin.Manifest
		if err := json.Unmarshal(definition.manifest, &manifest); err != nil {
			log.Printf("解析内置壁纸插件 manifest 失败: %v", err)
			continue
		}
		source, err := plugin.LoadSource(manifest, definition.filename, string(definition.source), dataDir)
		if err != nil {
			log.Printf("加载内置壁纸插件 %s 失败，继续使用 Go 壁纸源: %v", manifest.ID, err)
			continue
		}
		if err := service.ReplaceProvider(online.NewPluginProvider(source)); err != nil {
			source.Close()
			log.Printf("注册内置壁纸插件 %s 失败，继续使用 Go 壁纸源: %v", manifest.ID, err)
			continue
		}
		loaded = append(loaded, source)
		log.Printf("已加载内置壁纸插件: %s", fmt.Sprintf("%s@%s", manifest.ID, manifest.Version))
	}
	return loaded
}

func builtinPluginDefinitions() []embeddedPlugin {
	return []embeddedPlugin{
		{manifest: builtinBingManifest, filename: "index.js", source: builtinBingSource},
		{manifest: builtinQihooManifest, filename: "index.js", source: builtinQihooSource},
	}
}
