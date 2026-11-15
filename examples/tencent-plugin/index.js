// 腾讯视频：只做目录（L2）。搜索与频道接口都要签名，直连会被风控拒绝
// （ret 10401），所以这里不猜接口，只把入口交回官方页面。
const SITE = "https://v.qq.com";
const SEARCH = SITE + "/x/search/?q=";

// The id holds the page to open and the name to show; every card is an
// entrance, not a stream.
function openId(url, name) {
  return "open|" + encodeURIComponent(url) + "|" + encodeURIComponent(name || "");
}

function parseOpen(id) {
  const parts = String(id || "").split("|");
  return {
    url: decodeURIComponent(parts[1] || ""),
    name: decodeURIComponent(parts[2] || ""),
  };
}

function entry(name, url, overview) {
  return { id: openId(url, name), name, overview };
}

export async function home() {
  return {
    title: "腾讯视频",
    subtitle: "正片在官方页播放",
    sections: [
      {
        title: "频道",
        videos: [
          entry("电视剧", SITE + "/channel/tv", "在腾讯视频打开电视剧频道"),
          entry("电影", SITE + "/channel/movie", "在腾讯视频打开电影频道"),
          entry("综艺", SITE + "/channel/variety", "在腾讯视频打开综艺频道"),
          entry("动漫", SITE + "/channel/cartoon", "在腾讯视频打开动漫频道"),
          entry("纪录片", SITE + "/channel/documentary", "在腾讯视频打开纪录片频道"),
          entry("体育", SITE + "/channel/sports", "在腾讯视频打开体育频道"),
          entry("少儿", SITE + "/channel/children", "在腾讯视频打开少儿频道"),
        ],
      },
    ],
  };
}

export async function search({ query }) {
  const q = String(query || "").trim();
  if (!q) return { videos: [] };
  return {
    videos: [entry("在腾讯视频搜「" + q + "」", SEARCH + encodeURIComponent(q), "打开腾讯视频的搜索结果")],
  };
}

export function title({ id }) {
  const { url, name } = parseOpen(id);
  return {
    video: { id, name: name || url || "腾讯视频", overview: "这里只提供入口，播放交给官方页面。" },
    episodes: [],
  };
}

export function play({ id }) {
  const { url } = parseOpen(id);
  if (!url) return { drm: "找不到这个入口" };
  return { open: url };
}
