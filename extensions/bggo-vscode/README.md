# BGGo Wallpaper for VS Code

这个扩展为 VS Code 选择和管理本地壁纸。它会生成一份受 BGGo 管理的 CSS，并通过依赖扩展 **Custom CSS and JS Loader** 加载，不会直接修改 VS Code 安装文件。

## 本地开发

```bash
pnpm install
pnpm run check
pnpm run package
```

安装生成的 `.vsix` 后，从命令面板运行：

1. `BGGo: 选择 VS Code 壁纸`
2. 按提示重新加载窗口

透明度、模糊程度和对齐方式可在设置中搜索 `BGGo Wallpaper` 调整。

> Custom CSS and JS Loader 的工作方式会让 VS Code 显示“安装似乎损坏”的提示，这是加载工作台 CSS 的已知副作用。关闭壁纸后可重新安装 VS Code 恢复官方文件。

