# 壁纸插件开发指南

BGGo 壁纸插件采用和 MyGo Music 类似的目录插件结构。插件是一个包含 `plugin.json` 和 JavaScript/TypeScript 入口的 ZIP 包，由独立的 MoeJS runtime 加载。插件只负责提供分类和壁纸元数据，图片下载、缓存和桌面设置由宿主完成。

## 目录结构

```text
my-wallpaper-plugin/
├── plugin.json
└── index.js
```

`plugin.json` 是插件包根目录中唯一的 manifest。入口文件可以是 `.js` 或 `.ts`，`main` 默认为 `index.js`。

## Manifest

```json
{
  "type": "bggo-wallpaper",
  "apiVersion": 1,
  "id": "my-wallpaper",
  "name": "我的壁纸源",
  "version": "1.0.0",
  "minAppVersion": "0.2.10",
  "main": "index.js",
  "permissions": {
    "network": ["example.com"]
  },
  "settings": []
}
```

- `id` 是稳定标识，只允许字母、数字、点、连字符和下划线；发布更新时不能更改。
- `version` 用于显示和本地版本历史，建议使用 `主版本.次版本.修订版`。
- `minAppVersion` 可选，用于拒绝运行在过旧的 BGGo 上。
- `permissions.network` 必须列出插件请求 API 和返回图片 URL 使用的主机；未声明的主机会被拒绝。
- `settings` 当前用于描述插件设置元数据，运行时通过 `host.setting` 读取已保存的值。

## 入口能力

入口导出 `categories()` 和 `list()` 才能作为壁纸源使用；函数可以同步返回，也可以返回 Promise。

```js
export function categories() {
  return [{
    id: "featured",
    name: "精选",
    description: "我的壁纸源精选内容",
    badge: "NEW"
  }];
}

export async function list({ categoryId, page = 1, pageSize = 20 }) {
  const response = await host.fetch(
    `https://example.com/wallpapers?category=${categoryId}&page=${page}&size=${pageSize}`
  );
  const data = await response.json();
  return {
    items: data.items.map((item) => ({
      id: String(item.id),
      title: item.title || "未命名壁纸",
      thumbUrl: item.thumbnail,
      fullUrl: item.image,
      width: item.width,
      height: item.height,
      resolution: item.resolution,
      author: item.author,
      copyright: item.copyright
    })),
    more: Boolean(data.more),
    total: Number(data.total || 0)
  };
}
```

可选导出：

- `daily()`：返回每日壁纸，供宿主的每日同步使用。
- `random({ categoryId, pageSize })`：返回随机壁纸，供随机壁纸功能使用。

每个壁纸项至少需要 `id` 和 `fullUrl`。宿主会过滤不符合网络权限的图片地址，并为项目 ID 自动添加插件 ID 前缀。

## 宿主 API

- `host.fetch(url, options)`：发起网络请求，只能访问 manifest 声明的主机。优先使用 HTTPS。
- `host.setting(key)`：读取插件设置，未设置时返回空字符串。
- `host.storage.get(key)`、`host.storage.set(key, value)`、`host.storage.remove(key)`：读写只属于当前插件的 JSON 存储。
- `host.log(...args)`：写入宿主日志，不能用于向用户展示错误。

插件不能访问文件系统、启动进程、直接设置壁纸，也不能读取其他插件的数据。不要把远程图片下载到插件目录；只返回 URL，由宿主统一下载和缓存。

## 打包、安装和更新

从插件目录执行：

```bash
zip -r my-wallpaper-plugin.zip plugin.json index.js
```

然后在 BGGo 的“壁纸插件”页面选择“上传插件包”。宿主会在写入插件目录前检查 manifest、入口代码、路径穿越、符号链接、文件数量和解压大小。外部插件默认需要用户明确启用。

同一个 `id` 再次安装会被视为更新：旧版本会保存在用户配置目录的 `.history/<id>` 中。管理页可以查看历史数量并回滚到上一版；回滚只替换插件代码，不删除 `plugin-data/<id>` 中的设置和存储。

本地插件文件位于：

```text
配置目录/bggo/plugins/<id>/
配置目录/bggo/plugins/.history/<id>/
配置目录/bggo/plugin-data/<id>/
```

## 签名插件目录和远程更新

插件管理页支持从签名目录检查更新，但默认不连接任何在线市场。部署目录服务时设置以下环境变量后，应用才会显示远程更新：

```text
BGGO_PLUGIN_CATALOG_URL=https://downloads.example.com/bggo/plugins.json
BGGO_PLUGIN_CATALOG_PUBLIC_KEY=<Ed25519 公钥的 base64>
```

目录使用固定的 envelope 格式，签名覆盖 `payload` 的紧凑 JSON 编码：

```json
{
  "payload": {
    "version": 1,
    "generatedAt": "2026-11-12T00:00:00Z",
    "plugins": [{
      "manifest": {
        "type": "bggo-wallpaper",
        "apiVersion": 1,
        "id": "my-wallpaper",
        "name": "我的壁纸源",
        "version": "1.1.0",
        "main": "index.js",
        "permissions": {"network": ["example.com"]}
      },
      "url": "https://downloads.example.com/my-wallpaper-1.1.0.zip",
      "size": 1234,
      "sha256": "<64 位十六进制 SHA-256>",
      "signature": "<包 SHA-256 摘要的 Ed25519 签名 base64>"
    }]
  },
  "signature": "<payload JSON 的 Ed25519 签名 base64>"
}
```

目录、插件包和下载地址都必须通过 HTTPS。应用检查目录签名后，下载包还会检查文件大小、SHA-256、Ed25519 包签名以及包内 manifest 的 ID/版本；任何一步失败都不会进入插件安装器。签名私钥只能保存在发布服务器或受保护的 CI Secret 中，不能放入 BGGo 仓库或插件包。

检查到更新后，用户仍需在管理页确认安装；网络权限扩大也会在确认信息中明确提示。远程更新沿用本地 ZIP 的原子替换、历史保留和回滚机制。

## 本地调试建议

1. 先复制 `examples/wallpaper-plugin`，修改 `id`、名称和入口逻辑。
2. 用 `zip` 打包后从插件管理页安装，不要直接修改活动目录，避免绕过安装校验。
3. 先使用静态列表验证分类、分页和 URL 权限，再接入真实 API。
4. 更新 manifest 的 `permissions.network` 时，确认安装页面显示的权限变化符合预期。
5. 插件报错时先在管理页停用，再修改版本号重新安装；必要时使用回滚恢复上一版。

最小可运行示例位于 `examples/wallpaper-plugin`，其中的图片地址使用 `example.com` 占位，不会在打包或测试阶段访问外部网络。
