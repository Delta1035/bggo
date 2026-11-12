# 用户指南与故障排查

## 桌面壁纸

1. 启动 BGGo。
2. 点击“选择图片”，选择 JPG、JPEG、PNG、WebP 或 BMP 文件。
3. 在预览区确认图片，按需切换覆盖、适应或拉伸预览方式。
4. 点击“应用到桌面”。

“最近使用”最多保留 20 条记录。BGGo 只保存图片路径，不复制或上传原图。

> 当前的预览方式只影响应用内预览，尚未控制操作系统的填充方式。

### 在线图库

在线图库由内置壁纸插件提供。当前内置 Bing 每日精选和 360 壁纸源；插件负责分类、列表和图片地址解析，BGGo 宿主负责下载、缓存、图片校验和设置系统壁纸。内置插件加载失败时会自动回退到兼容实现，不影响本地壁纸功能。

在“插件管理”页面可以上传包含 `plugin.json` 的 ZIP 插件包，安装前会展示网络权限，安装后可以启用、停用、更新、回滚和卸载。更新时旧版本会保存在本地历史记录中，插件卡片会显示可回滚版本数量。插件包保存在 `bggo/plugins/<插件 ID>`，历史版本保存在 `bggo/plugins/.history/<插件 ID>`，运行数据保存在 `bggo/plugin-data/<插件 ID>`；卸载插件不会删除运行数据，回滚也不会清理设置和存储。更新插件如果扩大网络权限，会在确认卡片中单独提示。当前版本不提供在线插件市场。

### 本地数据

最近使用记录保存在操作系统返回的用户配置目录下：

- Linux 通常为 `~/.config/bggo/library.json`。
- Windows 通常为 `%AppData%\bggo\library.json`。

删除这个文件可以清空历史记录。执行前建议先退出 BGGo；原始图片不会被删除。

如果历史配置 JSON 损坏，BGGo 会自动把原文件改名为带时间戳的 `.corrupt-*` 备份，并以空的历史记录继续启动。需要排查时可以保留该备份文件后再提交问题报告。

## VS Code 壁纸

### 启用

可以通过以下两种方式之一设置 VS Code 壁纸：

- **方式一（从桌面端一键映射）**：
  在 BGGo 桌面端中选择壁纸后，点击“**映射到 VS Code**”按钮即可直接同步。如果勾选了“**同步到 VS Code**”，则每次点击“应用到桌面”时都会自动将壁纸映射到 VS Code 背景。
- **方式二（从 VS Code 命令面板设置）**：
  1. 安装 `BGGo Wallpaper` 扩展。
  2. 打开命令面板，运行 `BGGo: 选择 VS Code 壁纸`。
  3. 选择本地图片。

Linux 用户如果首次使用看到工作台写权限授权提示，检查说明后选择“一键授权并继续”。之后按提示重新加载 VS Code 窗口生效。

在设置中搜索 `BGGo Wallpaper` 可以修改：

- `bggo.wallpaper.opacity`：透明度，范围 `0.03`～`1`。
- `bggo.wallpaper.blur`：模糊半径，范围 `0`～`30` 像素。
- `bggo.wallpaper.position`：居中、顶部、底部、左侧或右侧。

修改设置后扩展会自动重新生成壁纸脚本。没有生效时运行 `BGGo: 重新生成壁纸样式` 并重新加载窗口。

### 关闭和恢复

运行 `BGGo: 关闭 VS Code 壁纸`，然后按提示重新加载窗口。这会自动清理工作台中的壁纸注入，不会删除原始图片。

如果 VS Code 仍提示安装损坏，或希望完全恢复官方工作台文件，请关闭壁纸后使用系统包管理器重装/修复 VS Code。不要手动删除不确定用途的工作台文件。

## 常见问题

### Linux 桌面提示“不支持当前桌面环境”

检查会话环境和对应命令是否存在：

```bash
printf '%s\n' "$XDG_CURRENT_DESKTOP" "$DESKTOP_SESSION"
command -v gsettings
command -v plasma-apply-wallpaperimage
command -v xfconf-query
```

GNOME、Ubuntu、Cinnamon 和 MATE 通常需要 `gsettings`；KDE Plasma 需要 `plasma-apply-wallpaperimage`；XFCE 需要 `xfconf-query`。只需安装当前桌面环境所需的命令。

### VS Code 报 `EACCES: permission denied`

运行 `BGGo: 修复 VS Code 壁纸权限`。自动授权要求系统安装 `pkexec` 和 `setfacl`。Debian/Ubuntu 缺少 `setfacl` 时可安装 `acl` 软件包：

```bash
sudo apt install acl
```

如果不希望自动授权，选择“复制手动命令”，检查命令指向当前 VS Code 安装目录后再执行。BGGo 使用 ACL 只为当前用户开放检测到的工作台入口文件和目录。

### 提示无法加载 `vscode-userdata:`

当前扩展会把旧的 `vscode-userdata:` 导入自动迁移为 `file:` 导入。运行 `BGGo: 重新生成壁纸样式`，再重新加载窗口。如果仍然出现，检查用户设置中的 `vscode_custom_css.imports`，确认没有其他扩展留下的失效地址。

### 命令成功但图片没有显示

依次检查：

1. 原始图片仍存在，并且当前用户可读。
2. `Custom CSS and JS Loader` 已启用，没有其他同类扩展同时接管工作台样式。
3. 运行 `BGGo: 重新生成壁纸样式` 后重新加载窗口。
4. VS Code 刚升级时，先运行权限修复，再重新生成样式。
5. 临时禁用其他自定义 CSS、主题增强或工作台注入扩展，排除冲突。

### VS Code 显示“安装似乎损坏”

这是 `Custom CSS and JS Loader` 修改工作台文件后的已知副作用，不代表 BGGo 修改了源代码或上传了数据。确认 VS Code 来源和扩展可信后可以隐藏该提示；如果不接受此行为，请关闭壁纸并修复/重装 VS Code。

## 报告问题时请提供

- BGGo 桌面版本或 VS Code 扩展版本。
- 操作系统、发行版、桌面环境及版本。
- VS Code 版本和安装方式（deb、rpm、snap、Flatpak 等）。
- 可以复现问题的步骤和完整错误文本。
- 是否安装其他自定义 CSS/JS 或壁纸扩展。

请不要提交个人图片、用户名、主目录完整路径、访问令牌或其他敏感信息。
