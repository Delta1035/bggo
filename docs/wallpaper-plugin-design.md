# 壁纸源插件设计

## 目标

将在线壁纸源从桌面应用的 Go 代码中解耦，使新增或修复壁纸源不必修改主界面和重新发布整个应用。插件只负责分类、列表和图片地址解析；下载、缓存、图片安全校验和系统壁纸设置仍由 BGGo 宿主负责。

本设计参考 [MyGo Music](https://github.com/egoist/mygo-music) 的目录插件模式：一个插件包含 `plugin.json` 和 JS/TS 入口文件，插件按导出的能力接入宿主。BGGo 不直接依赖 MyGo Music 的 `internal/plugin`，因为它属于另一个 Go module 的内部包；BGGo 将实现自己的壁纸协议和宿主运行时。

## 边界

插件可以：

- 通过宿主提供的 `fetch` 请求 manifest 声明的远程域名。
- 返回分类、壁纸元数据、缩略图地址和原图地址。
- 使用自己的设置和持久化存储。
- 按需刷新有时效性的图片地址。

插件不能：

- 读取 BGGo、其他插件或用户图片目录中的文件。
- 直接设置系统壁纸或修改 VS Code。
- 启动任意进程。
- 把远程图片内容直接写入任意本地路径。
- 访问未在 manifest 中声明的网络主机。

## 目录和 manifest

插件目录结构：

```text
plugins/
└── bing/
    ├── plugin.json
    ├── index.js
    └── locales/
        └── zh-CN.json
```

第一版 manifest：

```json
{
  "type": "bggo-wallpaper",
  "apiVersion": 1,
  "id": "bing",
  "name": "Bing Wallpapers",
  "version": "1.0.0",
  "minAppVersion": "0.2.10",
  "main": "index.js",
  "permissions": {
    "network": ["cn.bing.com"]
  },
  "settings": []
}
```

`id` 是稳定的源标识，不能随着显示名称改变。壁纸项和分类在宿主内部使用 `pluginID:itemID`、`pluginID:categoryID` 的命名空间，避免不同源返回相同 ID 时互相覆盖。

## JavaScript API

所有导出函数都接收一个参数对象，可以返回值或 Promise。没有导出的能力不在 UI 中显示。

### `categories`

```js
export async function categories() {
  return [{ id: "daily", name: "Bing 每日精选" }];
}
```

### `list`

```js
export async function list({ categoryId, page, pageSize }) {
  return {
    items: [{
      id: "20261112",
      title: "Wallpaper title",
      thumbUrl: "https://...",
      fullUrl: "https://...",
      width: 1920,
      height: 1080,
      resolution: "1920x1080",
      copyright: "..."
    }],
    more: false,
    total: 1
  };
}
```

### 可选能力

- `resolve({ id, variant })`：刷新过期或带签名的图片地址。
- `daily()`：返回每日壁纸，供自动 Bing 壁纸使用。
- `search({ query, page, pageSize })`：搜索壁纸，第一版不强制实现。
- `random({ categoryId, pageSize })`：返回随机壁纸，替代宿主硬编码 360 分类。

图片下载不通过插件导出函数完成。宿主拿到 URL 后统一执行域名检查、超时、大小限制、原子缓存和图片解码保护。

## 宿主 API

第一版只提供壁纸源需要的最小 API：

```js
host.fetch(url, options)
host.setting(key)
host.storage.get(key)
host.storage.set(key, value)
host.storage.remove(key)
host.log(...args)
host.language()
```

网络请求必须经过宿主：

- 目标域名必须匹配 manifest 的 `permissions.network`。
- HTTP 重定向后的最终域名也必须重新检查。
- 默认拒绝 `file:`, `localhost`、环回地址和内网地址。
- API 响应设置大小上限，并继承请求取消和超时。
- 360 这类历史 HTTP 源应优先迁移到 HTTPS；无法迁移时必须显式记录风险。

插件设置和存储隔离在：

```text
配置目录/bggo/plugin-data/<plugin-id>/settings.json
配置目录/bggo/plugin-data/<plugin-id>/storage.json
```

插件不能访问其他插件目录或 `library.json`。

## Go 宿主架构

当前 `online.Provider` 会保留作为兼容接口，并新增注册中心：

```text
WallpaperService
└── SourceRegistry
    ├── BuiltinSourceAdapter  (迁移过渡)
    └── PluginSource           (JS/TS 插件)
```

注册中心负责稳定 ID、注册顺序和冲突检查。`online.Service` 负责源路由、缓存和下载，不再直接通过 `switch` 判断 Bing 或 360。UI 只使用分类和源元数据，不判断具体源名称。

插件运行时单独放在 `internal/plugin`，负责：

- manifest 校验和最低版本检查。
- JS/TS 编译与懒加载。
- `host` API 注入。
- 每次调用的超时、内存上限和错误转换。
- 插件卡死后的关闭与下一次调用重启。

参考 MyGo Music 的运行时约束，初始值可以采用 30 秒调用超时和 256 MB JS runtime 内存上限；这些值应在实现时通过常量集中管理。

## 内置源和外部源

分两阶段迁移：

1. 先把 Bing 和 360 迁移为随应用发布的内置插件，验证协议、运行时和数据兼容。
2. 协议稳定后，再开放本地目录、ZIP 和 GitHub 插件安装，并增加权限确认、来源记录、更新和删除。

内置插件建议位于 `resources/plugins/bing` 和 `resources/plugins/qihoo`。外部插件使用用户配置目录中的 `plugins`。内置源 ID 固定为 `bing` 和 `360`，以保持已有配置、自动 Bing 功能和缓存兼容。

## 兼容和迁移

- 保留 `library.json` 和现有壁纸缓存目录。
- 暂不重命名已有 JSON 字段 `source`，先保持旧数据可读。
- 保留 `bing_daily` 和 360 分类 ID。
- `runAutoBingCheck` 改为调用 `bing` 插件的 `daily` 能力。
- 随机壁纸改为调用插件 `random`，不再在主程序中写死 360 分类 `9`。
- UI 分页使用插件返回的 `more` 和统一请求参数，不再判断 `cat.Source == "bing"`。
- 缓存 key 继续包含图片 URL，并在后续版本加入源 ID，避免源之间发生碰撞。

## 安全和错误隔离

插件是可执行逻辑，不能仅按普通 JSON 内容看待。安装和运行需要：

- manifest 的严格字段和路径校验。
- 插件入口必须位于自己的目录内。
- ZIP 解包拒绝路径穿越、符号链接和超出文件数量/大小限制的包。
- 插件错误、超时和 malformed 返回只禁用当前源，不影响其他源和本地壁纸功能。
- 插件权限在安装前展示；权限扩大时更新必须重新确认。
- 后续正式开放第三方插件前增加 SHA-256，最终增加 Ed25519 插件包签名。

## 实施阶段

### Phase 0：协议

保留本文作为协议基线，补充 manifest、返回值、错误码和版本兼容测试夹具。

### Phase 1：注册中心

新增 `SourceRegistry`，让当前 Go 版 Bing/360 通过同一注册路径提供分类和列表，保持现有 UI 行为不变。

### Phase 2：运行时

引入 MoeJS，实现 `internal/plugin`、真实 JS 调用链、网络权限、设置、存储、超时和内存限制。

### Phase 3：内置源迁移（已实现）

已将 Bing/360 逻辑迁移到 `resources/plugins` 中的内置 JS 插件。应用启动时优先注册插件 provider，失败时保留 Go fallback；每日同步、随机壁纸、宿主下载和插件返回的分页元数据均已接入，并覆盖真实 MoeJS 调用链与 provider 映射测试。旧分类对象仍保留展示兼容分支，待外部插件协议稳定后移除。

### Phase 4：插件管理（已实现）

已增加应用内“插件管理”入口，支持本地 ZIP 上传、安装前权限确认、原子安装、启用/停用、同 ID 更新、历史版本和回滚、卸载。安装器限制文件数量、解压大小、入口编译结果，并拒绝路径穿越和符号链接；卸载只移除插件包，保留插件运行数据。应用还会记录插件来源、版本、安装时间和已确认网络权限；更新扩大网络权限时会单独提示。开发者指南和可直接打包的静态示例位于 `docs/plugin-development.md` 和 `examples/wallpaper-plugin`。在线插件市场、远程更新和签名校验仍待后续实现。

后续继续增加在线市场、远程更新和签名校验。

每个阶段都必须通过真实插件 runtime 测试、`go test -race ./...`、`go vet ./...` 和 `go tool mygo vet`。
