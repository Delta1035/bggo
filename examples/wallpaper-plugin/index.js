export function categories() {
  return [{
    id: "sample",
    name: "示例壁纸",
    description: "用于验证插件安装、启用和回滚流程的静态示例"
  }];
}

export function list({ pageSize = 2 } = {}) {
  const items = [
    {
      id: "sample-01",
      title: "示例山谷",
      fullUrl: "https://example.com/bggo/sample-01.jpg",
      thumbUrl: "https://example.com/bggo/sample-01-thumb.jpg",
      resolution: "1920x1080"
    },
    {
      id: "sample-02",
      title: "示例海岸",
      fullUrl: "https://example.com/bggo/sample-02.jpg",
      thumbUrl: "https://example.com/bggo/sample-02-thumb.jpg",
      resolution: "1920x1080"
    }
  ];
  return { items: items.slice(0, Math.max(Number(pageSize) || 2, 1)), more: false, total: items.length };
}

export function random() {
  return list({ pageSize: 1 });
}
