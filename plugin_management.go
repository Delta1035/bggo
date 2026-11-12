package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"bggo/internal/online"
	"bggo/internal/plugin"
	"github.com/egoist/mygo"
)

type managedPlugin struct {
	Manifest plugin.Manifest
	Dir      string
	Builtin  bool
	Enabled  bool
	Source   *plugin.Plugin
	Error    string
}

func (a *application) initPluginManagement() {
	a.pluginRecords = make(map[string]*managedPlugin)
	a.pluginState = plugin.EnabledState{}
	configDir, err := os.UserConfigDir()
	if err != nil {
		a.pluginStatus = "无法确定插件配置目录: " + err.Error()
		return
	}
	baseDir := filepath.Join(configDir, "bggo")
	a.pluginRoot = filepath.Join(baseDir, "plugins")
	a.pluginDataRoot = filepath.Join(baseDir, "plugin-data")
	a.pluginStatePath = filepath.Join(baseDir, "plugin-state.json")
	state, err := plugin.LoadEnabledState(a.pluginStatePath)
	if err != nil {
		a.pluginStatus = "读取插件状态失败: " + err.Error()
		return
	}
	a.pluginState = state
}

func (a *application) registerBuiltinPluginRecords() {
	for _, definition := range builtinPluginDefinitions() {
		var manifest plugin.Manifest
		if err := json.Unmarshal(definition.manifest, &manifest); err != nil {
			continue
		}
		a.pluginRecords[manifest.ID] = &managedPlugin{
			Manifest: manifest,
			Builtin:  true,
			Enabled:  a.pluginEnabledByDefault(manifest.ID),
			Error:    "内置插件尚未启动",
		}
	}
	for _, source := range a.plugins {
		manifest := source.Manifest()
		record := a.pluginRecords[manifest.ID]
		if record == nil {
			record = &managedPlugin{Manifest: manifest, Builtin: true}
			a.pluginRecords[manifest.ID] = record
		}
		if record.Enabled {
			record.Source = source
			record.Error = ""
		} else {
			source.Close()
			a.onlineSvc.RemoveProvider(manifest.ID)
			record.Source = nil
			record.Error = "已停用"
		}
	}
}

func (a *application) loadInstalledPluginRecords() {
	if a.pluginRoot == "" || a.onlineSvc == nil {
		return
	}
	entries, err := plugin.ScanDirectory(a.pluginRoot)
	if err != nil {
		a.pluginStatus = "扫描插件目录失败: " + err.Error()
		return
	}
	for _, entry := range entries {
		id := entry.Manifest.ID
		if id == "" {
			id = filepath.Base(entry.Dir)
			entry.Manifest = plugin.Manifest{ID: id, Name: "无效插件", Version: "-"}
		}
		if _, builtin := a.pluginRecords[id]; builtin {
			entryErr := "插件 ID 与内置插件冲突"
			a.pluginRecords[id].Error = entryErr
			continue
		}
		record := &managedPlugin{
			Manifest: entry.Manifest,
			Dir:      entry.Dir,
			Enabled:  a.externalPluginEnabled(id),
			Error:    errorText(entry.Err),
		}
		a.pluginRecords[id] = record
		if entry.Err == nil && record.Enabled {
			if err := plugin.CheckCompatibility(record.Manifest, AppVersion); err != nil {
				record.Error = err.Error()
				record.Enabled = false
			} else {
				a.activateInstalledPlugin(record)
			}
		} else if entry.Err == nil {
			record.Error = "已停用"
		}
	}
}

func (a *application) pluginEnabledByDefault(id string) bool {
	enabled, ok := a.pluginState[id]
	return !ok || enabled
}

func (a *application) externalPluginEnabled(id string) bool {
	enabled, ok := a.pluginState[id]
	return ok && enabled
}

func (a *application) activateInstalledPlugin(record *managedPlugin) {
	source, err := plugin.Load(record.Dir, a.pluginDataRoot)
	if err != nil {
		record.Error = err.Error()
		record.Enabled = false
		return
	}
	if err := a.onlineSvc.RegisterProvider(online.NewPluginProvider(source)); err != nil {
		source.Close()
		record.Error = err.Error()
		record.Enabled = false
		return
	}
	record.Source = source
	record.Error = ""
	a.plugins = append(a.plugins, source)
}

func (a *application) activatePlugin(record *managedPlugin) error {
	if a.onlineSvc == nil {
		return errors.New("在线壁纸服务未初始化")
	}
	if record.Source != nil {
		return nil
	}
	var source *plugin.Plugin
	var err error
	if record.Builtin {
		for _, definition := range builtinPluginDefinitions() {
			var manifest plugin.Manifest
			if json.Unmarshal(definition.manifest, &manifest) == nil && manifest.ID == record.Manifest.ID {
				source, err = plugin.LoadSource(manifest, definition.filename, string(definition.source), a.pluginDataRoot)
				break
			}
		}
	} else {
		source, err = plugin.Load(record.Dir, a.pluginDataRoot)
	}
	if err != nil {
		record.Error = err.Error()
		return err
	}
	if err := a.onlineSvc.RegisterProvider(online.NewPluginProvider(source)); err != nil {
		source.Close()
		record.Error = err.Error()
		return err
	}
	record.Source = source
	record.Enabled = true
	record.Error = ""
	a.plugins = append(a.plugins, source)
	return nil
}

func (a *application) setPluginEnabled(id string, enabled bool) error {
	record := a.pluginRecords[id]
	if record == nil {
		return fmt.Errorf("未找到插件: %s", id)
	}
	if enabled {
		if err := a.activatePlugin(record); err != nil {
			return err
		}
		record.Enabled = true
		a.pluginState[id] = true
	} else {
		if a.onlineSvc != nil {
			a.onlineSvc.RemoveProvider(id)
		}
		if record.Source != nil {
			record.Source.Close()
			record.Source = nil
		}
		record.Enabled = false
		record.Error = "已停用"
		a.pluginState[id] = false
	}
	if err := plugin.SaveEnabledState(a.pluginStatePath, a.pluginState); err != nil {
		return fmt.Errorf("保存插件状态失败: %w", err)
	}
	return nil
}

func (a *application) choosePluginArchive() {
	if a.window == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:  a.window,
			Title:   "选择壁纸插件包",
			Message: "选择包含 plugin.json 和入口文件的 ZIP 插件包",
			Filters: []mygo.FileFilter{{Name: "BGGo 插件包", Extensions: []string{"zip"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		manifest, err := plugin.InspectArchive(paths[0])
		a.update(func() {
			if err != nil {
				a.pluginStatus = "插件包校验失败: " + err.Error()
				a.error = true
				return
			}
			if err := plugin.CheckCompatibility(manifest, AppVersion); err != nil {
				a.pluginStatus = "插件兼容性校验失败: " + err.Error()
				a.error = true
				return
			}
			a.pendingPluginZip = paths[0]
			a.pendingManifest = &manifest
			a.pluginStatus = "已读取插件信息，请确认权限后安装"
			a.error = false
		})
	}()
}

func (a *application) installPendingPlugin() {
	if a.pendingManifest == nil || a.pendingPluginZip == "" || a.pluginRoot == "" {
		return
	}
	zipPath := a.pendingPluginZip
	manifest := *a.pendingManifest
	if _, builtin := a.pluginRecords[manifest.ID]; builtin {
		a.pluginStatus = fmt.Sprintf("插件 ID %q 已被内置插件占用", manifest.ID)
		a.error = true
		return
	}
	a.update(func() {
		a.busy = true
		a.pluginStatus = "正在安装插件…"
		a.error = false
	})
	go func() {
		installed, err := plugin.InstallArchive(zipPath, a.pluginRoot)
		if err == nil {
			a.update(func() {
				err = a.installPluginRecord(installed)
			})
		}
		a.update(func() {
			a.busy = false
			if err != nil {
				a.pluginStatus = "插件安装失败: " + err.Error()
				a.error = true
				return
			}
			a.pendingPluginZip = ""
			a.pendingManifest = nil
			a.pluginStatus = "插件已安装并启用: " + manifest.Name
			a.error = false
		})
	}()
}

func (a *application) installPluginRecord(manifest plugin.Manifest) error {
	if err := plugin.CheckCompatibility(manifest, AppVersion); err != nil {
		return err
	}
	if _, builtin := a.pluginRecords[manifest.ID]; builtin {
		return fmt.Errorf("插件 ID %q 已被内置插件占用", manifest.ID)
	}
	previousRecord := a.pluginRecords[manifest.ID]
	var oldSource *plugin.Plugin
	if previousRecord != nil {
		if previousRecord.Source != nil && a.onlineSvc != nil {
			a.onlineSvc.RemoveProvider(manifest.ID)
			oldSource = previousRecord.Source
			previousRecord.Source = nil
		}
	}
	record := &managedPlugin{
		Manifest: manifest,
		Dir:      filepath.Join(a.pluginRoot, manifest.ID),
		Enabled:  true,
	}
	a.pluginRecords[manifest.ID] = record
	a.pluginState[manifest.ID] = true
	a.activateInstalledPlugin(record)
	if record.Error != "" {
		activationErr := errors.New(record.Error)
		if oldSource != nil {
			previousRecord.Source = oldSource
			previousRecord.Enabled = true
			previousRecord.Error = ""
			a.pluginRecords[manifest.ID] = previousRecord
			_ = a.onlineSvc.RegisterProvider(online.NewPluginProvider(oldSource))
		} else if previousRecord == nil {
			delete(a.pluginRecords, manifest.ID)
		} else {
			a.pluginRecords[manifest.ID] = previousRecord
		}
		return activationErr
	}
	if err := plugin.SaveEnabledState(a.pluginStatePath, a.pluginState); err != nil {
		if a.onlineSvc != nil {
			a.onlineSvc.RemoveProvider(manifest.ID)
		}
		if record.Source != nil {
			record.Source.Close()
		}
		if oldSource != nil {
			previousRecord.Source = oldSource
			previousRecord.Enabled = true
			previousRecord.Error = ""
			a.pluginRecords[manifest.ID] = previousRecord
			_ = a.onlineSvc.RegisterProvider(online.NewPluginProvider(oldSource))
		} else {
			if previousRecord == nil {
				delete(a.pluginRecords, manifest.ID)
			} else {
				a.pluginRecords[manifest.ID] = previousRecord
			}
		}
		return err
	}
	if oldSource != nil {
		oldSource.Close()
	}
	return nil
}

func (a *application) uninstallPlugin(id string) {
	record := a.pluginRecords[id]
	if record == nil || record.Builtin {
		return
	}
	if a.onlineSvc != nil {
		a.onlineSvc.RemoveProvider(id)
	}
	if record.Source != nil {
		record.Source.Close()
	}
	if err := plugin.RemoveInstalled(a.pluginRoot, id); err != nil {
		a.pluginStatus = "卸载插件失败: " + err.Error()
		a.error = true
		return
	}
	delete(a.pluginRecords, id)
	delete(a.pluginState, id)
	if err := plugin.SaveEnabledState(a.pluginStatePath, a.pluginState); err != nil {
		a.pluginStatus = "插件已卸载，但状态保存失败: " + err.Error()
		a.error = true
		return
	}
	a.pluginStatus = "插件已卸载，插件数据已保留"
	a.error = false
}

func (a *application) pluginList() []*managedPlugin {
	result := make([]*managedPlugin, 0, len(a.pluginRecords))
	for _, record := range a.pluginRecords {
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Manifest.ID < result[j].Manifest.ID
	})
	return result
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func pluginNetworkText(manifest plugin.Manifest) string {
	if len(manifest.Permissions.Network) == 0 {
		return "无网络权限"
	}
	return fmt.Sprintf("网络权限：%s", joinStrings(manifest.Permissions.Network, ", "))
}

func joinStrings(values []string, separator string) string {
	result := ""
	for i, value := range values {
		if i > 0 {
			result += separator
		}
		result += value
	}
	return result
}
