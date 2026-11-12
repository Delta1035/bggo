const defaultBaseURL = "https://cn.bing.com";

function baseURL() {
  return (host.setting("baseUrl") || defaultBaseURL).replace(/\/$/, "");
}

function absoluteURL(value) {
  if (/^https?:\/\//i.test(value)) {
    return value;
  }
  return baseURL() + (value.startsWith("/") ? value : "/" + value);
}

function thumbnailURL(fullURL) {
  return fullURL + (fullURL.includes("?") ? "&" : "?") + "pid=hp&w=384&h=216";
}

function toItem(image) {
  const fullUrl = absoluteURL(image.url || "");
  let title = (image.title || "").trim();
  if (!title) title = (image.copyright || "").trim();
  if (!title) title = "Bing 每日壁纸";
  return {
    id: image.hsh || image.startdate || image.fullstartdate,
    title,
    thumbUrl: thumbnailURL(fullUrl),
    fullUrl,
    resolution: "1920x1080",
    copyright: image.copyright || ""
  };
}

export function categories() {
  return [{
    id: "bing_daily",
    name: "必应每日精选",
    description: "微软必应官方精选 · 每日自动同步 · 4K 超高清原图直连",
    badge: "8"
  }];
}

export async function list({ page = 1, pageSize = 8 }) {
  let count = Math.min(Math.max(Number(pageSize) || 8, 1), 8);
  let index = (Math.max(Number(page) || 1, 1) - 1) * count;
  index = Math.min(Math.max(index, 0), 7);
  const response = await host.fetch(`${baseURL()}/HPImageArchive.aspx?format=js&idx=${index}&n=${count}&mkt=zh-CN`);
  const result = await response.json();
  const items = (result.images || []).map(toItem).filter((item) => item.id && item.fullUrl);
  return { items, more: index + items.length < 8, total: 8 };
}

export async function daily() {
  return list({ page: 1, pageSize: 1 });
}
