// LibreTV：聚合搜索十三个苹果 CMS 采集站，取 m3u8 直连播放。详情一次请求
// 给出全部集数，播放不再回源。个别源的 m3u8 有防盗链（如极速），那种搜出
// 来换别的源就是。
const UA =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36";

// 以实测为准（2026-11）：死站、Cloudflare 挑战、关搜索的都剔了。
const SOURCES = [
  { key: "dyttzy", name: "电影天堂", api: "http://caiji.dyttzyapi.com/api.php/provide/vod" },
  { key: "ruyi", name: "如意", api: "https://cj.rycjapi.com/api.php/provide/vod" },
  { key: "bfzy", name: "暴风", api: "https://bfzyapi.com/api.php/provide/vod" },
  { key: "ffzy", name: "非凡", api: "http://ffzy5.tv/api.php/provide/vod" },
  { key: "zy360", name: "360", api: "https://360zy.com/api.php/provide/vod" },
  { key: "iqiyi", name: "iqiyi", api: "https://www.iqiyizyapi.com/api.php/provide/vod" },
  { key: "jisu", name: "极速", api: "https://jszyapi.com/api.php/provide/vod" },
  { key: "dbzy", name: "豆瓣", api: "https://dbzy.tv/api.php/provide/vod" },
  { key: "mdzy", name: "魔都", api: "https://www.mdzyapi.com/api.php/provide/vod" },
  { key: "zuid", name: "最大", api: "https://api.zuidapi.com/api.php/provide/vod" },
  { key: "yinghua", name: "樱花", api: "https://m3u8.apiyhzy.com/api.php/provide/vod" },
  { key: "ikun", name: "iKun", api: "https://ikunzyapi.com/api.php/provide/vod" },
  { key: "lzi", name: "量子", api: "https://cj.lziapi.com/api.php/provide/vod" },
];

const PER_PAGE = 20;
const SEARCH_TIMEOUT = 8000;

function sourceOf(key) {
  for (const s of SOURCES) {
    if (s.key === key) return s;
  }
  return null;
}

// The id carries the source and the episode: search gives src|vodId, an
// episode of a title gives src|vodId|index.
function encodeId(key, vodId, ep) {
  return ep === undefined ? key + "|" + vodId : key + "|" + vodId + "|" + ep;
}

function decodeId(id) {
  const parts = String(id || "").split("|");
  return { key: parts[0] || "", vodId: parts[1] || "", ep: parts[2] };
}

function withTimeout(p, ms) {
  return Promise.race([
    p,
    new Promise((_, reject) =>
      setTimeout(() => reject(new Error("超时")), ms)
    ),
  ]);
}

async function fetchJSON(api, params) {
  const url = api + "?ac=videolist&" + params;
  const res = await withTimeout(
    fetch(url, { headers: { "User-Agent": UA, Accept: "application/json" } }),
    SEARCH_TIMEOUT
  );
  if (!res.ok) throw new Error("返回 " + res.status);
  return await res.json();
}

// vod_play_url is "来源一$$$来源二", a source is "名$地址#名$地址".
function playSources(vod) {
  return String(vod.vod_play_url || "")
    .split("$$$")
    .map((block) =>
      block
        .split("#")
        .map((ep) => {
          const i = ep.indexOf("$");
          return i < 0
            ? { name: ep.trim(), url: ep.trim() }
            : { name: ep.slice(0, i).trim(), url: ep.slice(i + 1).trim() };
        })
        .filter((e) => /^https?:\/\//.test(e.url))
    )
    .filter((eps) => eps.length > 0);
}

// The m3u8 source plays here; a plain page source does not.
function bestSource(vod) {
  const sources = playSources(vod);
  let plain = null;
  for (const eps of sources) {
    if (String(eps[0].url).includes(".m3u8")) return eps;
    if (plain === null) plain = eps;
  }
  return plain;
}

function toVideo(key, vod) {
  return {
    id: encodeId(key, vod.vod_id),
    name: vod.vod_name || String(vod.vod_id),
    poster: vod.vod_pic || "",
    overview: vod.vod_remarks || "",
  };
}

async function searchOne(source, query, page) {
  const json = await fetchJSON(
    source.api,
    "wd=" + encodeURIComponent(query) + "&pg=" + page
  );
  const out = [];
  for (const vod of json.list || []) {
    if (vod && vod.vod_id !== undefined) out.push(toVideo(source.key, vod));
  }
  return out;
}

export async function home() {
  const cats = ["电影", "电视剧", "综艺", "动漫"];
  const sections = await Promise.all(
    cats.map(async (cat) => {
      try {
        const videos = await searchOne(SOURCES[1], cat, 1);
        return { title: cat, videos: videos.slice(0, 8) };
      } catch (e) {
        return { title: cat, videos: [] };
      }
    })
  );
  return {
    title: "LibreTV",
    sections: sections.filter((s) => s.videos.length > 0),
  };
}

export async function search({ query, offset }) {
  if (!query) return { videos: [] };
  const page = Math.floor((offset || 0) / PER_PAGE) + 1;
  const results = await Promise.allSettled(
    SOURCES.map((s) => searchOne(s, query, page))
  );
  const out = [];
  const seen = new Set();
  let any = false;
  for (const r of results) {
    if (r.status !== "fulfilled") continue;
    for (const v of r.value) {
      if (seen.has(v.id)) continue;
      seen.add(v.id);
      out.push(v);
    }
    if (r.value.length > 0) any = true;
  }
  return { videos: out, more: any && page < 50 };
}

export async function title({ id }) {
  const { key, vodId } = decodeId(id);
  const source = sourceOf(key);
  if (!source) throw new Error("未知来源 " + key);
  const json = await fetchJSON(source.api, "ids=" + encodeURIComponent(vodId));
  const vod = (json.list || [])[0];
  if (!vod) throw new Error("找不到这部影片");
  const eps = bestSource(vod) || [];
  const episodes = eps.map((e, i) => ({
    id: encodeId(key, vodId, i),
    name: e.name || "第" + (i + 1) + "集",
    episode: i + 1,
  }));
  return {
    video: {
      id: encodeId(key, vodId),
      name: vod.vod_name || vodId,
      poster: vod.vod_pic || "",
      overview: vod.vod_content || vod.vod_remarks || "",
    },
    episodes,
  };
}

export async function play({ id }) {
  const { key, vodId, ep } = decodeId(id);
  const source = sourceOf(key);
  if (!source) throw new Error("未知来源 " + key);
  const index = ep === undefined || ep === "" ? 0 : Number(ep);
  const json = await fetchJSON(source.api, "ids=" + encodeURIComponent(vodId));
  const vod = (json.list || [])[0];
  if (!vod) throw new Error("找不到这部影片");
  const eps = bestSource(vod) || [];
  const pick = eps[index] || eps[0];
  if (!pick) throw new Error("这一集没有播放地址");
  const url = String(pick.url);
  return {
    url,
    type: url.includes(".m3u8") ? "hls" : "mp4",
  };
}
