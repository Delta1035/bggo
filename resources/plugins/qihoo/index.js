const defaultBaseURL = "http://wallpaper.apc.360.cn";
const defaultCategoryURL = "http://cdn.apc.360.cn";

function baseURL() {
  return (host.setting("baseUrl") || defaultBaseURL).replace(/\/$/, "");
}

function categoryURL() {
  return (host.setting("categoryUrl") || defaultCategoryURL).replace(/\/$/, "");
}

function upgradeURL(value) {
  return (value || "").replace(/^http:\/\//i, "https://");
}

function imageVariants(value) {
  const raw = upgradeURL(value);
  return {
    thumbUrl: raw.replace("/bdr/__85/", "/bdm/480_270_80/"),
    fullUrl: raw.replace("/bdr/__85/", "/bdm/1920_1080_80/")
  };
}

function titleFor(item) {
  const tags = (item.utag || "").trim().split(/\s+/).filter(Boolean);
  if (tags.length >= 2) return `${tags[0]} · ${tags[1]}`;
  if (tags.length === 1) return tags[0];
  return `360壁纸 #${item.id || ""}`;
}

export async function categories() {
  const response = await host.fetch(`${categoryURL()}/index.php?c=WallPaper&a=getAllCategoriesV2&from=360chrome`);
  const result = await response.json();
  return (result.data || []).filter((item) => item.id && item.name).map((item) => ({
    id: String(item.id),
    name: item.name,
    description: "360壁纸库精选 · 国内 CDN 极速直连 · 高清多分辨率适配",
    badge: "在线"
  }));
}

export async function list({ categoryId, page = 1, pageSize = 20 }) {
  const count = Math.max(Number(pageSize) || 20, 1);
  const currentPage = Math.max(Number(page) || 1, 1);
  const start = (currentPage - 1) * count;
  const response = await host.fetch(`${baseURL()}/index.php?c=WallPaper&a=getAppsByCategory&cid=${encodeURIComponent(categoryId)}&start=${start}&count=${count}&from=360chrome`);
  const result = await response.json();
  const items = (result.data || []).map((item) => {
    const variants = imageVariants(item.url || item.url_thumb);
    return {
      id: `360_${item.id}`,
      title: titleFor(item),
      thumbUrl: variants.thumbUrl,
      fullUrl: variants.fullUrl,
      resolution: item.resolution || "1920x1080",
      copyright: "360壁纸库"
    };
  }).filter((item) => item.fullUrl);
  return { items, more: items.length >= count, total: Number(result.total) || 0 };
}

export async function random({ categoryId = "9", pageSize = 30 }) {
  return list({ categoryId, page: 1, pageSize });
}
