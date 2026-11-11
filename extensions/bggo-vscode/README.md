# BGGo Wallpaper for VS Code

这个扩展为 VS Code 选择和管理本地壁纸。它采用原生纯 CSS（`body::after`）安全注入工作台，**严格保留官方默认的 Content-Security-Policy (CSP) 安全策略**，不再依赖第三方 Custom CSS Loader，与 Codex、Copilot 等所有 Webview 扩展 100% 兼容。

## 本地开发

```bash
pnpm install
pnpm test
pnpm run package
```

安装生成的 `.vsix` 后，从命令面板运行：

1. `BGGo: 选择 VS Code 壁纸`
2. Linux 安装目录不可写时，点击“一键授权并继续”并完成系统授权
3. 按提示重新加载窗口

透明度、模糊程度和对齐方式可在设置中搜索 `BGGo Wallpaper` 调整。

Linux 上 BGGo 通过 `pkexec` 和 `setfacl` 只为当前用户开放 VS Code 的工作台目录和入口文件。授权失败时可运行 `BGGo: 修复 VS Code 壁纸权限`，或使用自动复制的手动命令。VS Code 升级重建工作台目录后可能需要重新授权。

> 修改工作台文件会让 VS Code 显示“安装似乎损坏”的校验提示，这是本地注入样式的已知副作用。点击提示框齿轮选择“不再显示”即可。

源码仓库中的 `docs/user-guide.md` 提供完整的使用、关闭和故障排查说明，`docs/architecture.md` 介绍架构与权限边界。
