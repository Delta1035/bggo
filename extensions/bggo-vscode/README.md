# BGGo Wallpaper for VS Code

这个扩展为 VS Code 选择和管理本地壁纸。它会生成一份受 BGGo 管理的工作台脚本，并通过依赖扩展 **Custom CSS and JS Loader** 加载。BGGo 不直接改写 VS Code 安装文件，但依赖扩展需要修改工作台文件才能完成注入。

## 本地开发

```bash
pnpm install
pnpm run check
pnpm run package
```

安装生成的 `.vsix` 后，从命令面板运行：

1. `BGGo: 选择 VS Code 壁纸`
2. Linux 安装目录不可写时，点击“一键授权并继续”并完成系统授权
3. 按提示重新加载窗口

透明度、模糊程度和对齐方式可在设置中搜索 `BGGo Wallpaper` 调整。

Linux 上 BGGo 通过 `pkexec` 和 `setfacl` 只为当前用户开放 VS Code 的工作台目录和入口文件。授权失败时可运行 `BGGo: 修复 VS Code 壁纸权限`，或使用自动复制的手动命令。VS Code 升级重建工作台目录后可能需要重新授权。

> Custom CSS and JS Loader 的工作方式会让 VS Code 显示“安装似乎损坏”的提示，这是加载工作台 CSS 的已知副作用。关闭壁纸后可重新安装 VS Code 恢复官方文件。

完整的使用、关闭和故障排查说明见 [`../../docs/user-guide.md`](../../docs/user-guide.md)，架构与权限边界见 [`../../docs/architecture.md`](../../docs/architecture.md)。

