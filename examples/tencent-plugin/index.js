// 腾讯视频：首页与频道用 getPage 接口取，正片在官方页面播放（目录）。
// 搜索接口要签名（ret 10401），所以搜索只把入口交回官方页面。
const API =
  "https://pbaccess.video.qq.com/trpc.vector_layout.page_view.PageService/getPage";
const SITE = "https://v.qq.com";
const SEARCH = SITE + "/x/search/?q=";
const REFERER = SITE + "/";
const UA =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36";

// page_id is what the site calls each channel; taken from its own requests.
const CHANNELS = [
  { id: "100101", name: "推荐" },
  { id: "100113", name: "电视剧" },
  { id: "100114", name: "电影" },
  { id: "100109", name: "综艺" },
  { id: "100119", name: "动漫" },
];

// A card names the album, and the album page is where playback happens.
function encodeId(cid, name) {
  return "c|" + cid + "|" + encodeURIComponent(name || "");
}

function decodeId(id) {
  const parts = String(id || "").split("|");
  return { cid: parts[1] || "", name: decodeURIComponent(parts[2] || "") };
}

function guid() {
  let s = "";
  for (let i = 0; i < 32; i++) s += Math.floor(Math.random() * 16).toString(16);
  return s;
}

const device = guid();

async function page(pageId) {
  const body = {
    page_params: {
      page_type: "channel",
      page_id: pageId,
      scene: "channel",
      new_mark_label_enabled: "1",
    },
    page_bypass_params: {
      params: {
        platform_id: "2",
        caller_id: "3000010",
        data_mode: "default",
        user_mode: "default",
        specified_strategy: "",
        page_type: "channel",
        page_id: pageId,
        scene: "channel",
        new_mark_label_enabled: "1",
      },
      scene: "channel",
      app_version: "",
      abtest_bypass_id: device,
    },
    page_context: null,
  };
  const url =
    API + "?video_appid=3000010&vversion_platform=2&vdevice_guid=" + device;
  const res = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Origin: SITE,
      Referer: REFERER,
      "User-Agent": UA,
    },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error("腾讯视频返回 " + res.status);
  return await res.json();
}

function picture(url) {
  if (!url) return "";
  if (url.startsWith("//")) return "https:" + url;
  return url.replace(/^http:/, "https:");
}

// Every block lists its cards in children_list.list.cards; some blocks have
// none, which is fine.
function videos(json) {
  const out = [];
  const seen = new Set();
  for (const block of (json.data && json.data.CardList) || []) {
    const list = (block.children_list && block.children_list.list) || {};
    const cards = Array.isArray(list.cards) ? list.cards : [];
    for (const card of cards) {
      const p = card.params || {};
      if (!p.title || !card.id || seen.has(card.id)) continue;
      seen.add(card.id);
      out.push({
        id: encodeId(card.id, p.title),
        name: p.title,
        poster: picture(p.image_url || p.cut_image_url || ""),
        overview: p.year ? String(p.year) : "",
      });
    }
  }
  return out;
}

export async function home() {
  const sections = await Promise.all(
    CHANNELS.slice(0, 4).map(async (ch) => {
      try {
        return { title: ch.name, videos: videos(await page(ch.id)).slice(0, 8) };
      } catch (e) {
        return { title: ch.name, videos: [] };
      }
    })
  );
  return { title: "腾讯视频", sections: sections.filter((s) => s.videos.length > 0) };
}

export async function browse({ page: n }) {
  const ch = CHANNELS[Math.min(Math.max(0, (n || 1) - 1), CHANNELS.length - 1)];
  return { title: ch.name, sections: [{ title: ch.name, videos: videos(await page(ch.id)) }] };
}

export async function search({ query }) {
  const q = String(query || "").trim();
  if (!q) return { videos: [] };
  return {
    videos: [
      {
        id: "open|" + encodeURIComponent(SEARCH + encodeURIComponent(q)),
        name: "在腾讯视频搜「" + q + "」",
        overview: "搜索接口要签名，这里打开官方搜索",
      },
    ],
  };
}

export function title({ id }) {
  if (String(id).startsWith("open|")) {
    return {
      video: { id, name: "在腾讯视频搜索", overview: "打开官方搜索页。" },
      episodes: [],
    };
  }
  const { cid, name } = decodeId(id);
  return {
    video: { id, name: name || cid, overview: "在腾讯视频的官方页面播放。" },
    episodes: [],
  };
}

export function play({ id }) {
  if (String(id).startsWith("open|")) {
    return { open: decodeURIComponent(String(id).split("|")[1] || "") };
  }
  const { cid } = decodeId(id);
  if (!cid) return { drm: "找不到这个视频" };
  return { open: SITE + "/x/cover/" + cid + ".html" };
}
