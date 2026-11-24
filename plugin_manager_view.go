package main

import (
	"fmt"

	"bggo/internal/icons"
	"bggo/internal/plugin"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/harukizmoe/mayu"
)

func (a *application) drawPluginManager(c *ui.Context, t *ui.Theme) {
	scheme := mayu.Scheme(c)
	ui.Column(c).Grow(1).FillHeight().Padding(20, 22, 18, 22).Gap(14).Background(scheme.Surface).Children(func() {
		ui.Row(c).AlignItems(ui.Center).FillWidth().DragWindow().Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, "壁纸插件管理").FontSize(20).Bold().TextColor(scheme.OnSurface)
				ui.Text(c, "像 MyGo Music 一样，通过插件扩展壁纸源").FontSize(12).TextColor(scheme.OnSurfaceVariant)
			})
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				check := ui.ButtonBase(c.Key("plugin-check-updates")).Height(34).Padding(0, 12).Radius(8).Background(scheme.SurfaceContainerLow).Border(1, scheme.OutlineVariant)
				check.Children(func() {
					ui.Text(c, "检查更新").FontSize(12).Bold().TextColor(scheme.OnSurface)
				})
				if check.Clicked() && !a.busy {
					a.checkPluginUpdates()
				}
				upload := ui.ButtonBase(c.Key("plugin-upload")).Height(34).Padding(0, 14).Radius(8).Background(scheme.Primary)
				upload.Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Icon(c, icons.Download).Size(14, 14).TextColor(scheme.Surface)
						ui.Text(c, "上传插件包").FontSize(12).Bold().TextColor(scheme.Surface)
					})
				})
				if upload.Clicked() {
					a.choosePluginArchive()
				}
			})
		})

		ui.Box(c).FillWidth().Padding(12, 14).Radius(10).Background(scheme.SurfaceContainerLow).Border(1, scheme.OutlineVariant).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Box(c).Width(30).Height(30).Radius(8).Background(scheme.PrimaryContainer).Center().Children(func() {
					ui.Icon(c, icons.Download).Size(15, 15).TextColor(scheme.Primary)
				})
				ui.Column(c).Grow(1).Gap(3).Children(func() {
					ui.Text(c, "安全安装本地插件").FontSize(13).Bold().TextColor(scheme.OnSurface)
					ui.Text(c, "选择 ZIP 后会先校验 manifest、入口代码、文件大小和权限；确认安装后才会写入插件目录。").FontSize(11).TextColor(scheme.OnSurfaceVariant).SingleLine()
				})
			})
		})

		if a.pendingManifest != nil {
			a.drawPendingPlugin(c)
		}

		if a.pluginStatus != "" {
			color := scheme.OnSurfaceVariant
			if a.error {
				color = scheme.Error
			}
			ui.Text(c, a.pluginStatus).FontSize(12).TextColor(color).SingleLine()
		}

		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).FillWidth().Children(func() {
			ui.Text(c, fmt.Sprintf("已登记 %d 个插件", len(a.pluginRecords))).FontSize(13).Bold().TextColor(scheme.OnSurface)
			ui.Text(c, "内置插件不可卸载，外部插件卸载时保留运行数据").FontSize(11).TextColor(scheme.OnSurfaceVariant)
		})

		ui.Scroll(c).Grow(1).FillWidth().Gap(10).Children(func() {
			plugins := a.pluginList()
			if len(plugins) == 0 {
				ui.Column(c).Fill().Center().Gap(8).Children(func() {
					ui.Text(c, "还没有插件").FontSize(16).Bold().TextColor(t.TextMuted)
					ui.Text(c, "上传一个包含 plugin.json 的 ZIP 开始扩展壁纸源").FontSize(12).TextColor(t.TextMuted)
				})
			}
			for _, record := range plugins {
				a.drawPluginCard(c, record)
			}
		})
	})
}

func (a *application) drawPendingPlugin(c *ui.Context) {
	scheme := mayu.Scheme(c)
	manifest := a.pendingManifest
	ui.Box(c).FillWidth().Padding(12, 14).Radius(10).Background(ui.Hex("#FFFBEB")).Border(1, ui.Hex("#FCD34D")).Children(func() {
		ui.Column(c).Gap(7).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, "安装前确认").FontSize(13).Bold().TextColor(ui.Hex("#92400E"))
					ui.Text(c, fmt.Sprintf("%s · %s · %s", manifest.Name, manifest.ID, manifest.Version)).FontSize(12).TextColor(ui.Hex("#78350F"))
				})
				ui.Text(c, "待确认").FontSize(11).Bold().TextColor(ui.Hex("#B45309"))
			})
			ui.Text(c, pluginNetworkText(*manifest)).FontSize(11).TextColor(ui.Hex("#78350F"))
			if a.pendingPermissionExpanded {
				ui.Text(c, "此更新会扩大网络权限，确认后才会替换当前版本。").FontSize(11).Bold().TextColor(ui.Hex("#B45309"))
			}
			ui.Text(c, "插件代码会在独立 MoeJS runtime 中运行；安装后仍可随时停用。").FontSize(11).TextColor(ui.Hex("#92400E"))
			ui.Row(c).Gap(8).Children(func() {
				confirm := mayu.FilledButton(c, "确认安装").OnClick(func() {
					if !a.busy {
						a.installPendingPlugin()
					}
				})
				confirm.Height(36)
				cancel := ui.ButtonBase(c.Key("plugin-install-cancel")).Height(30).Padding(0, 12).Radius(6).Background(scheme.Surface).Border(1, ui.Hex("#FCD34D"))
				cancel.Children(func() {
					ui.Text(c, "取消").FontSize(11).TextColor(ui.Hex("#92400E"))
				})
				if cancel.Clicked() && !a.busy {
					a.pendingPluginZip = ""
					a.pendingManifest = nil
					a.pluginStatus = "已取消插件安装"
				}
			})
		})
	})
}

func (a *application) drawPluginCard(c *ui.Context, record *managedPlugin) {
	scheme := mayu.Scheme(c)
	border := scheme.OutlineVariant
	if record.Error != "" {
		border = ui.Hex("#FECACA")
	}
	ui.Box(c).FillWidth().Padding(14, 16).Radius(10).Background(scheme.Surface).Border(1, border).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Box(c).Width(38).Height(38).Radius(10).Background(ui.Hex("#EFF6FF")).Center().Children(func() {
				ui.Icon(c, icons.Image).Size(18, 18).TextColor(scheme.Primary)
			})
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, record.Manifest.Name).FontSize(14).Bold().TextColor(scheme.OnSurface)
					ui.Text(c, record.Manifest.ID).FontSize(10).TextColor(scheme.OnSurfaceVariant)
				})
				sourceText := pluginSourceLabel(record)
				if metadata, ok := a.pluginMetadata[record.Manifest.ID]; ok {
					if metadata.Source == "catalog" {
						sourceText = "在线目录"
					}
					if !metadata.InstalledAt.IsZero() {
						sourceText += " · " + metadata.InstalledAt.Local().Format("2006-01-02 15:04")
					}
				}
				ui.Text(c, fmt.Sprintf("版本 %s · %s", record.Manifest.Version, sourceText)).FontSize(11).TextColor(scheme.OnSurfaceVariant)
				ui.Text(c, pluginNetworkText(record.Manifest)).FontSize(11).TextColor(scheme.OnSurfaceVariant).SingleLine()
				if !record.Builtin {
					historyLabel := fmt.Sprintf("历史版本：%d", len(record.History))
					if len(record.History) > 0 {
						historyLabel += " · 上一版 " + record.History[0].Manifest.Version
					}
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, historyLabel).FontSize(11).TextColor(scheme.OnSurfaceVariant)
						rollback := ui.ButtonBase(c.Key("plugin-rollback-"+record.Manifest.ID)).Height(24).Padding(0, 8).Radius(5).Background(scheme.SurfaceContainerLow).Border(1, scheme.OutlineVariant).Disabled(len(record.History) == 0 || a.busy)
						rollback.Children(func() {
							ui.Text(c, "回滚上一版").FontSize(10).TextColor(scheme.OnSurface)
						})
						if rollback.Clicked() && len(record.History) > 0 && !a.busy {
							version := record.History[0]
							go a.confirmRollbackPlugin(record.Manifest.ID, record.Manifest.Name, version.ID, version.Manifest.Version)
						}
					})
					if update, ok := a.pluginUpdates[record.Manifest.ID]; ok {
						ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Text(c, "发现新版本 "+update.Manifest.Version).FontSize(11).Bold().TextColor(scheme.Primary)
							updateButton := ui.ButtonBase(c.Key("plugin-update-"+record.Manifest.ID)).Height(24).Padding(0, 8).Radius(5).Background(ui.Hex("#EFF6FF")).Border(1, ui.Hex("#BFDBFE")).Disabled(a.busy)
							updateButton.Children(func() {
								ui.Text(c, "更新").FontSize(10).Bold().TextColor(scheme.Primary)
							})
							if updateButton.Clicked() && !a.busy {
								go a.confirmRemotePluginUpdate(update)
							}
						})
					}
				}
				if record.Error != "" {
					ui.Text(c, "状态："+record.Error).FontSize(11).TextColor(scheme.Error).SingleLine()
				}
			})
			statusText := "已停用"
			statusColor := scheme.OnSurfaceVariant
			if record.Enabled && record.Error == "" {
				statusText = "运行中"
				statusColor = ui.Hex("#059669")
			}
			ui.Box(c).Padding(3, 8).Radius(12).Background(ui.Hex("#F1F5F9")).Children(func() {
				ui.Text(c, statusText).FontSize(10).Bold().TextColor(statusColor)
			})
			if !a.busy {
				toggle := ui.ButtonBase(c.Key("plugin-toggle-"+record.Manifest.ID)).Height(30).Padding(0, 10).Radius(6).Background(scheme.SurfaceContainerLow).Border(1, scheme.OutlineVariant)
				toggle.Children(func() {
					ui.Text(c, map[bool]string{true: "停用", false: "启用"}[record.Enabled]).FontSize(11).TextColor(scheme.OnSurface)
				})
				if toggle.Clicked() {
					if err := a.setPluginEnabled(record.Manifest.ID, !record.Enabled); err != nil {
						a.pluginStatus = "更新插件状态失败: " + err.Error()
						a.error = true
					} else {
						a.pluginStatus = fmt.Sprintf("插件%s：%s", map[bool]string{true: "已启用", false: "已停用"}[record.Enabled], record.Manifest.Name)
						a.error = false
					}
				}
			}
			if !record.Builtin && !a.busy {
				remove := ui.ButtonBase(c.Key("plugin-remove-"+record.Manifest.ID)).Height(30).Padding(0, 10).Radius(6).Background(ui.Hex("#FFF7F7")).Border(1, ui.Hex("#FECACA"))
				remove.Children(func() {
					ui.Icon(c, icons.Trash).Size(12, 12).TextColor(scheme.Error)
				})
				if remove.Clicked() {
					go a.confirmUninstallPlugin(record.Manifest.ID, record.Manifest.Name)
				}
			}
		})
	})
}

func (a *application) confirmUninstallPlugin(id, name string) {
	result, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.window,
		Type:               mygo.MessageWarning,
		Title:              "卸载插件",
		Message:            fmt.Sprintf("确定卸载 %s？", name),
		Detail:             "插件包会被移除，但运行数据会保留。",
		Buttons:            []string{"卸载", "取消"},
		DestructiveButtons: []int{0},
		CancelButton:       1,
	})
	if err == nil && result.Button == 0 {
		a.update(func() { a.uninstallPlugin(id) })
	}
}

func (a *application) confirmRollbackPlugin(id, name, versionID, version string) {
	result, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.window,
		Type:               mygo.MessageWarning,
		Title:              "回滚插件",
		Message:            fmt.Sprintf("确定将 %s 回滚到 %s？", name, version),
		Detail:             "当前版本会保留在历史记录中，插件运行数据不会删除。",
		Buttons:            []string{"回滚", "取消"},
		DestructiveButtons: []int{0},
		CancelButton:       1,
	})
	if err != nil || result.Button != 0 {
		return
	}
	a.update(func() {
		a.busy = true
		err := a.rollbackPlugin(id, versionID)
		a.busy = false
		if err != nil {
			a.pluginStatus = "插件回滚失败: " + err.Error()
			a.error = true
			return
		}
		a.pluginStatus = fmt.Sprintf("插件 %s 已回滚到 %s", name, version)
		a.error = false
	})
}

func (a *application) confirmRemotePluginUpdate(entry plugin.CatalogEntry) {
	permissionExpanded := a.permissionExpanded(entry.Manifest)
	detail := "插件包已通过签名目录校验，安装后仍可随时停用。"
	if permissionExpanded {
		detail = "此更新会扩大网络权限；插件包已通过签名目录校验，确认后才会安装。"
	}
	result, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.window,
		Type:               mygo.MessageWarning,
		Title:              "更新插件",
		Message:            fmt.Sprintf("确定将 %s 更新到 %s？", entry.Manifest.Name, entry.Manifest.Version),
		Detail:             detail,
		Buttons:            []string{"更新", "取消"},
		DestructiveButtons: []int{0},
		CancelButton:       1,
	})
	if err == nil && result.Button == 0 {
		a.updatePluginFromCatalog(entry)
	}
}

func pluginSourceLabel(record *managedPlugin) string {
	if record.Builtin {
		return "内置插件"
	}
	return "本地安装"
}
