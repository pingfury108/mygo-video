// 优酷：搜索、频道，正片在官方页播放。官方接口给不出封面与时长，卡片用
// 首字色块；播放按钮打开 v.youku.com 的页面（L2），不碰 DRM。
const SEARCH = "https://search.youku.com/api/search";
const PLAY_PAGE = "https://v.youku.com/v_show/id_";
const REFERER = "https://so.youku.com/";
const UA =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36";

// The id carries the title along: the search result is all we get, and the
// detail page has no second call to make.
function encodeId(videoId, title) {
  return "v|" + videoId + "|" + encodeURIComponent(title || "");
}

function decodeId(id) {
  const parts = String(id || "").split("|");
  return {
    videoId: parts[1] || "",
    title: decodeURIComponent(parts[2] || ""),
  };
}

async function searchJSON(query, page) {
  const url =
    SEARCH +
    "?keyword=" +
    encodeURIComponent(query) +
    "&pg=" +
    Math.max(1, page || 1);
  const res = await fetch(url, {
    headers: { "User-Agent": UA, Referer: REFERER },
  });
  if (!res.ok) throw new Error("优酷返回 " + res.status);
  return await res.json();
}

// The result list moves between component keys, so every component's data is
// walked for anything with a videoId.
function videos(json) {
  const out = [];
  const seen = new Set();
  for (const comp of json.pageComponentList || []) {
    const map = comp.componentMap || {};
    for (const key of Object.keys(map)) {
      for (const d of map[key].data || []) {
        if (!d || !d.videoId || seen.has(d.videoId)) continue;
        seen.add(d.videoId);
        const title = d.title || d.displayName || d.videoId;
        out.push({
          id: encodeId(d.videoId, title),
          name: title,
          poster: d.img || d.thumb || "",
        });
      }
    }
  }
  return out;
}

export async function home() {
  const cats = ["电影", "电视剧", "综艺", "动漫", "纪录片"];
  const sections = await Promise.all(
    cats.map(async (cat) => {
      try {
        const json = await searchJSON(cat, 1);
        return { title: cat, videos: videos(json).slice(0, 8) };
      } catch (e) {
        return { title: cat, videos: [] };
      }
    })
  );
  return {
    title: "优酷",
    sections: sections.filter((s) => s.videos.length > 0),
  };
}

export async function search({ query, offset }) {
  if (!query) return { videos: [] };
  const page = Math.floor((offset || 0) / 20) + 1;
  const json = await searchJSON(query, page);
  const list = videos(json);
  const end = json.pageData && json.pageData.isEnd;
  return { videos: list, more: end === 0 };
}

export function title({ id }) {
  const { videoId, title } = decodeId(id);
  return {
    video: {
      id,
      name: title || videoId,
      overview: "在优酷的官方页面播放。",
    },
    episodes: [],
  };
}

export function play({ id }) {
  const { videoId } = decodeId(id);
  if (!videoId) return { drm: "找不到这个视频" };
  return { open: PLAY_PAGE + videoId + ".html" };
}
