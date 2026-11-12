# 开发指南

## 环境要求

- Go `1.27.3`。启用 `GOTOOLCHAIN=auto` 时 Go 可按 `go.mod` 获取匹配工具链。
- MyGo `v0.7.4`，已通过 `tool` 指令声明，不需要全局安装。
- Node.js：使用仍受维护且满足依赖要求的版本。
- pnpm `11.5.2`，扩展必须使用 pnpm，不提交 npm 或 Yarn 锁文件。
- Linux 构建需要 MyGo 和目标桌面环境对应的系统依赖。

## 桌面应用

安装依赖并运行检查：

```bash
go mod download
go test -race ./...
go vet ./...
go tool mygo vet
```

开发模式：

```bash
go tool mygo dev
```

`mygo dev` 提供开发期热重载。它不是生产环境的自动更新机制。

构建当前平台：

```bash
go tool mygo build
```

构建当前项目已经验证过的目标：

```bash
go tool mygo build -platform linux/amd64,windows/amd64
```

### 壁纸插件

内置壁纸源位于 `resources/plugins/<id>`，由 `plugin.json` 和 `index.js` 组成。入口导出 `categories()`、`list()`，可选导出 `daily()` 或 `random()`；网络请求必须使用 `host.fetch`，且目标主机必须写入 manifest 的 `permissions.network`。图片地址也必须属于声明主机，否则宿主会丢弃该条目。

插件包使用用户配置目录下的 `bggo/plugins/<id>`，运行数据单独保存在 `bggo/plugin-data/<id>`，启停状态保存在 `bggo/plugin-state.json`。不要把设置、缓存或运行时生成文件写入仓库。新增内置源时应同时保留同 ID 的 Go fallback、补充真实 MoeJS 调用链测试，并验证插件失败时其他源仍可用。

重新生成图标：

```bash
go run ./tools/genicon resources/icon.png
```

## VS Code 扩展

```bash
cd extensions/bggo-vscode
pnpm install --frozen-lockfile
pnpm run check
pnpm run package
pnpm audit --prod
```

`pnpm run package` 会先编译 TypeScript，再生成 `.vsix`。`dist/`、`.vsix` 和 `node_modules/` 都是构建产物，不应提交。

本地安装生成的包后，应至少手工验证：选择图片、调整设置、重新生成、关闭壁纸，以及 VS Code 重启后的状态。Linux 还需要验证授权取消、授权成功和 VS Code 升级后恢复。

## 分支和提交

- 一个任务使用一个独立分支或 worktree，建议命名为 `feat/...`、`fix/...`、`docs/...` 或 `chore/...`。
- 不修改或提交任务范围外的文件，尤其注意共享工作区中的用户改动。
- 提交前先展示 `git status`、变更摘要和测试结果，并等待项目负责人确认。
- 提交信息建议使用 Conventional Commits，例如 `feat(vscode): ...`、`fix(linux): ...`。
- 涉及网站或其他前端技术栈时，必须先由项目负责人确认技术栈。

## 完成定义

代码任务只有同时满足以下条件才算完成：

- 行为符合任务验收标准，失败路径有可理解的提示。
- 新逻辑有适当的自动测试，平台相关行为有手工验证记录。
- Go 变更通过 `go test -race ./...`、`go vet ./...` 和 `go tool mygo vet`。
- 扩展变更通过 `pnpm run check`；发布相关变更还需完成打包和生产依赖审计。
- 用户可见行为、配置、权限或兼容性变化已更新文档。
- 没有提交构建产物、个人配置、密钥或任务外改动。

## 测试矩阵记录格式

平台测试不要只写“已测试”，建议在任务或 PR 中记录：

```text
系统/版本：Ubuntu 24.04
桌面/会话：GNOME 46 / Wayland
安装来源：本地 deb
场景：选择 PNG → 应用 → 注销登录后检查
结果：通过/失败
证据：错误文本、日志或截图位置
已知偏差：无
```
