# 发布与更新方案

仓库现已配置 GitHub Actions CI、Release 草稿工作流与桌面自动更新。代码签名、扩展商店发布及正式版推广仍是后续工作。工作流需推送到 GitHub 后验证首次运行；本地构建通过不代表 GitHub 托管环境已经验证。

## 已实现的 CI 与打包

- `.github/workflows/ci.yml`：分支 push、PR 和手动运行时触发，也供发布工作流复用。
- 桌面应用在 Ubuntu 24.04 和 Windows Server 2022 的 amd64 runner 上分别执行格式检查、依赖校验、竞态测试、Go vet、MyGo vet 和安装包构建。
- 扩展使用 Node.js 24 和 `package.json` 中指定的 pnpm，通过冻结锁文件安装、TypeScript 检查、VSIX 打包和生产依赖审计。
- 构建产物作为 Actions artifacts 保留 14 天。普通 CI 只有仓库读取权限，不创建 Release。
- Go 版本直接读取 `go.mod`。MyGo 会生成 Linux `.deb`、`.tar.gz` 和 `install.sh`，Windows NSIS 安装程序；Windows runner 缺少 NSIS 时 MyGo 会自动下载。
- `scripts/package-desktop.py` 校验预期安装包存在，并将 Windows 可执行程序和资源文件打成便携 ZIP。

## 如何发测试版

1. 使用 `python3 scripts/bump-version.py X.Y.Z` 同步修改 `mygo.json`、`extensions/bggo-vscode/package.json` 和 `main.go`，并补充 `CHANGELOG.md`。
2. 提交修改并确认 CI 通过，然后在对应提交上打 Tag。例如：

   ```bash
   # 只有准备发布该版本时才创建 Tag
   git tag v0.2.10
   git push origin v0.2.10
   ```

3. `.github/workflows/release.yml` 校验 Tag 与三个版本来源完全一致，并重新运行完整 CI。当前接受数字形式的 `vX.Y.Z` Tag；Alpha 状态通过 GitHub prerelease 标记表达，暂不接受带后缀的 Tag。
4. 自动生成 **draft + prerelease**，包含桌面端、扩展、`SHA256SUMS.txt` 和记录提交 SHA/Actions 链接的 `BUILD-INFO.txt`。这些信息不是代码签名或构建来源认证。
5. 下载并校验文件（Linux 使用 `sha256sum --check SHA256SUMS.txt`），完成安装、启动、升级和卸载测试，编辑发布说明，再手工发布草稿。当前产物未签名，应保留 prerelease 标记。

工作流仅发布阶段具有 `contents: write`，使用 GitHub 自带的 `GITHUB_TOKEN`，不需要额外 PAT。建议为发布 Tag 设置 repository ruleset，限制创建、修改和删除权限，并为默认分支启用 CI 必须通过的规则。仓库组织策略必须允许工作流创建 Release。

同一 Tag 的并发发布会串行执行；已有 Release 时会报错，不覆盖已有附件。若草稿创建后需重试，先检查内容，确认是失败的草稿后删除草稿再重跑；已公开版本应使用新版本号。

## 打包发版调研结论

直接复用 MyGo 已有打包能力即可，无需引入另一套桌面打包框架。当前先支持 Linux/Windows amd64，不宣称已验证其他架构或 GUI 真机兼容性。

- **Windows 签名**：MyGo 支持签名配置；后续需要证书、受保护的签名凭据及时间戳服务，便携程序与安装程序均需签名。
- **扩展商店**：VSIX 可先离线分发。Marketplace 需要确认 `bggo` publisher 所有权和发布凭据；Open VSX 需要对应 namespace 与令牌。建议后续用受保护 environment 单独执行商店发布。
- **自动更新**：已接入 MyGo 原生更新体系（`mygo.json` 已配置 `updates` 且集成了 `github.com/egoist/mygo/plugins/updater/native` 原生更新窗口）。通过 `go tool mygo keygen` 生成 Ed25519 签名密钥对，在构建时通过环境变量 `MYGO_UPDATER_PRIVATE_KEY` 签署产物并生成 `update-<target>.json` 清单；应用支持每日后台静默检查与手动菜单/界面按钮触发更新检查。
- **正式发版**：补齐签名、真机验证和维护记录后再建立稳定渠道。以下是后续目标设计，不代表当前已经实现。

## 版本策略

当前采用统一版本：

- 桌面应用版本由 `mygo.json` 管理。
- VS Code 扩展版本由 `extensions/bggo-vscode/package.json` 管理。
- Git Tag 使用 `vX.Y.Z`，同时代表桌面应用和 VS Code 扩展。

两个组件暂时一起发布；如果未来需要独立版本，必须同时修改本文、CI、更新源和发布脚本，避免同一 Tag 指向含义不同的产物。

版本遵循 SemVer：

- `MAJOR`：产生需要用户迁移的不兼容行为。
- `MINOR`：向后兼容的新功能。
- `PATCH`：向后兼容的修复。

Alpha/Beta 可使用 `0.x.y` 和预发布后缀。每次发布都维护 `CHANGELOG.md`，内容至少包含新增、修复、已知问题和升级注意事项。

## 发布产物

### 桌面应用

首批目标产物：

- Linux amd64：`.deb` 和 `.tar.gz`。
- Windows amd64：安装程序和便携包（是否同时提供由发布任务确认）。
- 每个文件的 SHA-256 校验和。
- 可验证的构建来源和发布说明。

进入稳定版前应增加代码签名。无法签名的测试包必须明确标记，不得伪装成稳定版。

### VS Code 扩展

- `.vsix` 离线安装包。
- VS Code Marketplace 发布。
- Open VSX 发布。
- 发布页注明依赖扩展、工作台修改机制和“安装似乎损坏”提示。

## 发布流水线

建议把 CI 与 Release 分开：

1. 每次提交执行格式、静态检查、单元测试和可构建性检查。
2. 受保护 Tag 触发发布构建。
3. 在干净环境生成产物，写入版本和提交 SHA。
4. 对产物签名并生成 SHA-256。
5. 先发布到草稿或预发布渠道，完成 Linux/Windows 冒烟测试。
6. 人工批准后转为正式 Release，并发布扩展商店。
7. 发布后验证下载、安装、启动、升级和卸载。

密钥只能存储在 CI 的受保护 Secret 中；来自外部贡献分支的流水线不能访问签名或商店令牌。

## 自动更新

### 桌面应用更新

生产环境不采用任意代码“热更新”。推荐流程是：

```text
客户端读取 HTTPS 更新清单
  → 验证清单签名
  → 比较平台、架构、版本和更新通道
  → 用户确认下载
  → 校验包哈希与签名
  → 调用平台安装/替换流程
  → 失败时保留旧版本并可回滚
```

至少支持 `stable` 和 `beta` 两个通道。默认只检查稳定通道，并允许用户关闭自动检查。下载和安装应分别征得用户同意，除非之后明确设计静默更新策略。

更新清单至少包含：版本、通道、发布日期、最低兼容版本、平台、架构、下载地址、文件大小、SHA-256、签名和发布说明地址。

### 内容热更新

在线壁纸目录、分类、公告和推荐可以通过签名 JSON 清单更新，不随应用发版。内容清单只能描述数据和静态资源，不允许携带或执行 JS、Shell、Go 或其他代码。

内容系统需要：

- 清单签名与密钥轮换策略。
- 图片来源、许可证、作者和原始链接。
- 缓存上限、清理和离线降级。
- 下载校验、超时、取消和失败重试。
- 内容下架、版本兼容和隐私说明。

### VS Code 扩展更新

优先使用 Marketplace / Open VSX 自带更新机制，不自行实现扩展代码更新。扩展升级需要保留用户配置，并验证旧的 `vscode_custom_css.imports` 能安全迁移。

## 回滚和恢复

- Release 页面至少保留一个可用旧版本及其校验和。
- 自动更新失败不得删除当前可运行版本。
- 配置迁移必须先备份或设计为向后兼容。
- 内容清单解析失败时使用最后一次验证通过的缓存。
- VS Code 壁纸关闭后应能移除 BGGo 导入；完整恢复工作台文件依赖 VS Code 修复或重装流程。

## 正式发布检查清单

- [ ] 版本号、Tag 和 `CHANGELOG.md` 一致。
- [ ] 干净环境的完整检查通过。
- [ ] Linux 和 Windows 兼容矩阵达到发布门槛。
- [ ] 安装、升级、降级、卸载和恢复完成冒烟测试。
- [ ] 产物签名与 SHA-256 可验证。
- [ ] 发布说明列出已知问题和破坏性变化。
- [ ] 下载页、用户文档和扩展商店信息同步更新。
- [ ] 发布后监控反馈，并明确紧急撤回负责人。
