# BGGo

BGGo 是一个基于 [MyGo](https://github.com/egoist/mygo) 原生 UI 的跨平台壁纸工具。当前优先支持 Linux，Windows 已提供系统壁纸适配；VS Code 壁纸由仓库中的独立扩展负责。

## 当前功能

- 选择并预览 JPG、PNG、WebP、BMP 图片
- 支持覆盖、适应、拉伸三种预览方式
- 保存最近使用的壁纸
- Linux：GNOME/Ubuntu、KDE Plasma、XFCE、Cinnamon、MATE
- Windows：通过系统 API 设置桌面壁纸
- VS Code：通过 `extensions/bggo-vscode` 选择、启用和关闭编辑器壁纸

## 开发环境

MyGo `v0.5.5` 需要 Go `1.27.2+`。仓库声明了 Go 工具链版本，启用 `GOTOOLCHAIN=auto` 时 Go 可以自动获取匹配的工具链。

```bash
go mod download
go test ./...
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

