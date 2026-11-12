# 架构说明

## 仓库结构

```text
bggo/
├── main.go                         # MyGo 窗口、状态和交互
├── internal/library/               # 最近使用记录
├── internal/wallpaper/             # 参数校验和系统适配
├── extensions/bggo-vscode/         # 独立 VS Code 扩展
├── resources/                      # 应用图标与内置壁纸插件
│   └── plugins/                    # Bing、360 的 plugin.json 与 JS 入口
├── internal/plugin/                # MoeJS 插件运行时与宿主 API
├── internal/online/                # 源注册中心、provider 与缓存
├── tools/genicon/                  # 图标生成工具
├── mygo.json                       # 桌面应用元数据
└── docs/                            # 项目文档
```

## 桌面应用

桌面应用使用 MyGo 原生 UI。`application` 持有窗口、预览图、当前选择、最近记录和操作状态。

```text
用户选择图片
  → main.go 读取并解码预览
  → wallpaper.Service 校验路径和扩展名
  → 当前操作系统适配器设置壁纸
  → library.Store 写入最近记录
```

### Linux 适配

`internal/wallpaper/system_linux.go` 根据 `XDG_CURRENT_DESKTOP` 和 `DESKTOP_SESSION` 选择命令：

| 桌面环境 | 接口 |
| --- | --- |
| GNOME / Ubuntu | `gsettings`，同时尝试亮色和深色壁纸键 |
| Cinnamon | `gsettings` |
| MATE | `gsettings` |
| KDE Plasma | `plasma-apply-wallpaperimage` |
| XFCE | `xfconf-query`，更新所有 `last-image` 项 |

无法识别桌面环境时会依次尝试 GNOME、KDE 和 XFCE 接口。所有外部命令都以参数数组执行，不经过 Shell。

### Windows 适配

`internal/wallpaper/system_windows.go` 调用 `user32.dll` 的 `SystemParametersInfoW`，并要求系统持久化设置和广播变更。

### 本地状态

`internal/library.Store` 使用 `os.UserConfigDir()` 定位 `bggo/library.json`，文件权限为 `0600`。写入采用同目录临时文件、同步和原子替换；读取到损坏 JSON 时会先将原文件改名为带时间戳的 `.corrupt-*` 备份，再以空状态继续运行。

## 在线壁纸插件

在线源统一通过 `internal/online.SourceRegistry` 注册。Bing 和 360 的兼容 Go provider 仍保留为回退实现，启动时优先加载 `resources/plugins` 中的嵌入式 JS 插件；插件编译、注册或运行失败时，应用继续使用对应 Go provider。

```text
resources/plugins/<id>
  → plugin.LoadSource
  → MoeJS 独立 runtime
  → online.PluginProvider
  → SourceRegistry
  → UI / 下载缓存 / 系统壁纸
```

插件只能通过 manifest 声明的网络主机使用 `host.fetch`。插件返回的原图和缩略图 URL 在进入宿主下载流程前会再次校验协议和声明主机，插件包保存在 `bggo/plugins/<id>`，设置与存储分别保存到 `bggo/plugin-data/<id>/settings.json` 和 `storage.json`。插件来源、版本、安装时间和已确认网络权限保存到 `plugin-metadata.json`；更新扩大网络权限时需要在管理页再次确认。每个插件按需启动，调用超时或关闭后下一次调用会创建新的 runtime。

## VS Code 扩展

扩展是独立的 TypeScript 包，不嵌入桌面应用。它将生成的纯 CSS 写入扩展全局存储目录，并通过自身安全注入引擎直接挂载到 VS Code 工作台（`workbench.html`）。

样式通过纯 CSS `body::after` 创建固定背景图层，图片自身使用 VS Code 沙箱允许的 `vscode-file://vscode-app/...` URI。注入过程**严格保留官方默认的 Content-Security-Policy (CSP) 安全策略**，不删除任何 CSP 标签，彻底避免干扰 Codex、Copilot 等依赖 Webview 的 AI 与工具扩展。

```text
选择图片
  → 保存全局配置
  → 生成全局存储中的纯 CSS 样式
  → 检查/修复工作台权限
  → 安全注入工作台（保留原生 CSP）
  → 重新加载工作台
```

### Linux 权限边界

扩展只在 Linux 检查权限。它基于 `vscode.env.appRoot` 查找已知工作台入口，解析真实路径并验证目标仍位于应用目录内，然后通过固定路径的 `pkexec` 和 `setfacl` 为当前 UID 添加 ACL。命令不经过 Shell。

已知边界：

- VS Code 更新可能重建目录并丢失 ACL。
- snap、Flatpak 和第三方 VS Code 分支尚未形成完整兼容结论。
- 关闭 BGGo 壁纸不会自动撤销 ACL；恢复策略需要单独设计和验证。

## 安全与隐私

- 桌面应用的在线图库会请求 Bing、360 壁纸接口和用户选择的图片地址；本地壁纸与 VS Code 扩展流程不会上传用户图片。
- 用户图片保持在原位置，应用只读取图片和保存绝对路径。
- 扩展请求提权时不接收、记录或传递用户密码。
- 未来的内容更新清单必须签名，并且不得包含任意 JS、Shell 或 Go 代码。

## 需要优先改善的边界

- 把 UI 状态、图片解码、配置持久化和系统调用进一步解耦，便于测试。
- 图片读取和解码已移出 UI 更新回调，并加入 64 MB 文件大小和 4000 万像素上限；后续仍可增加取消机制。
- 配置已采用原子写入并处理损坏文件恢复；并发写入仍需进一步验证。
- 系统适配器改为可注入接口，以便覆盖 Linux 命令选择和 Windows 调用测试。
- 为 VS Code 权限定位、URI 迁移和配置合并补单元测试。
- 外部插件目录扫描、安装、更新和权限确认仍未开放；当前只发布内置插件，并保留 Go fallback。
