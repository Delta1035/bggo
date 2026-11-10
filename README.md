# BGGo

BGGo 是一个基于 [MyGo](https://github.com/egoist/mygo) 原生 UI 的跨平台壁纸工具。当前优先支持 Linux，Windows 已提供系统壁纸适配；VS Code 壁纸由仓库中的独立扩展负责。

> 当前阶段：Alpha。核心流程已经可用，但尚未完成完整的真机兼容测试、正式发布验证、签名和自动更新，不建议作为稳定版分发。

## 当前功能

- 选择并预览 JPG、PNG、WebP、BMP 图片
- 支持覆盖、适应、拉伸三种预览方式
- 保存最近使用的壁纸
- Linux：GNOME/Ubuntu、KDE Plasma、XFCE、Cinnamon、MATE
- Windows：通过系统 API 设置桌面壁纸
- VS Code 联动：支持在桌面端一键将壁纸映射到 VS Code 插件背景，或勾选自动同步
- VS Code 扩展：通过 `extensions/bggo-vscode` 选择、启用和关闭编辑器壁纸

## 文档

- [文档导航](docs/README.md)
- [产品范围与当前状态](docs/product.md)
- [用户指南与故障排查](docs/user-guide.md)
- [架构说明](docs/architecture.md)
- [开发指南](docs/development.md)
- [发布与更新方案](docs/release-and-update.md)
- [路线图与 Agent 任务卡](docs/roadmap.md)

## 开发环境

MyGo `v0.5.5` 需要 Go `1.27.2+`。仓库声明了 Go 工具链版本，启用 `GOTOOLCHAIN=auto` 时 Go 可以自动获取匹配的工具链。

```bash
go mod download
go test -race ./...
go vet ./...
go tool mygo vet
go tool mygo dev
```

构建当前平台安装包：

```bash
go tool mygo build
```

重新生成应用图标：

```bash
go run ./tools/genicon resources/icon.png
```

VS Code 扩展的开发与打包方法见 `extensions/bggo-vscode/README.md`。


## CI 与测试版发布

GitHub Actions 在分支提交和 PR 时检查并打包桌面应用及 VS Code 扩展。推送 `desktop-vX.Y.Z` 或 `vscode-vX.Y.Z` Tag 后会生成带校验和的 Release 草稿；版本必须匹配组件配置。操作步骤与签名、商店发布的后续安排见 [发布与更新方案](docs/release-and-update.md)。
