// LibreTV：聚合搜索采集站，取 m3u8 直连播放。源清单从部署的 tv 站运行时
// 拉取——换源改那边即可，无需重发插件；拉不到时退回内置清单。详情一次
// 请求给出全部集数，播放不再回源。个别源的 m3u8 有防盗链，那种搜出来
// 换别的源就是。
const UA =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36";

// 内置兑底：tv 站的源清单拉不到时用。
const BUILT_IN = [
  { key: "ruyi", name: "如意", api: "https://cj.rycjapi.com/api.php/provide/vod" },
  { key: "bfzy", name: "暴风", api: "https://bfzyapi.com/api.php/provide/vod" },
  { key: "zy360", name: "360", api: "https://360zy.com/api.php/provide/vod" },
  { key: "jisu", name: "极速", api: "https://jszyapi.com/api.php/provide/vod" },
  { key: "lzi", name: "量子", api: "https://cj.lziapi.com/api.php/provide/vod" },
];

// 源清单的远端地址与缓存：tv 站托管着与网页同一份清单文件，改源只改那边。
const LIST_URL = "https://tv.pingfury.top/js/customer_site.js";
const LIST_KEY = "sites";
const LIST_TTL = 12 * 60 * 60 * 1000;

let sources = null;
let sourcesAt = 0;

// parseSites reads the entries of a customer_site.js, whose shape this
// deployment keeps stable: key: { api: '…', name: '…', … }.
function parseSites(js) {
  const out = [];
  const re = /(\w+)\s*:\s*\{[^{}]*?api:\s*['"]([^'"]+)['"][^{}]*?name:\s*['"]([^'"]+)['"][^{}]*?\}/g;
  let m;
  while ((m = re.exec(String(js))) !== null) {
    out.push({ key: m[1], name: m[3], api: m[2] });
  }
  return out;
}

async function loadSources() {
  if (sources && Date.now() - sourcesAt < LIST_TTL) return sources;
  try {
    const cached = JSON.parse(host.storage.get(LIST_KEY) || "null");
    if (cached && cached.list && cached.list.length && Date.now() - cached.at < LIST_TTL) {
      sources = cached.list;
      sourcesAt = cached.at;
      return sources;
    }
  } catch (e) {}
  try {
    const res = await withTimeout(fetch(LIST_URL, { headers: { "User-Agent": UA } }), 6000);
    if (res.ok) {
      const list = parseSites(await res.text());
      if (list.length >= 3) {
        sources = list;
        sourcesAt = Date.now();
        host.storage.set(LIST_KEY, JSON.stringify({ at: sourcesAt, list }));
        return sources;
      }
    }
  } catch (e) {}
  if (!sources) {
    sources = BUILT_IN;
    sourcesAt = Date.now();
  }
  return sources;
}

const PER_PAGE = 20;
const SEARCH_TIMEOUT = 8000;

// What the player plays: the source's own address, with the relay as a
// template the app's proxy wraps every fetch in — the playlist's relative
// addresses resolve against the source, and each fetch carries the secret
// anew.
function stream(url) {
  const server = (host.setting("relayServer") || "").trim().replace(/\/$/, "");
  const password = (host.setting("relayPassword") || "").trim();
  if (!server || !password) {
    return { url, type: url.includes(".m3u8") ? "hls" : "mp4" };
  }
  const auth = host.crypto.sha256(password);
  return {
    url,
    type: "hls",
    relay: server + "/proxy/%s?auth=" + auth,
  };
}

// The site's front page: Douban's hot lists. The plugin asks Douban
// directly, which the browser could not (no CORS), and bridges a card to
// the collection sites by searching for its title.
const DOUBAN = "https://movie.douban.com/j/search_subjects";

async function doubanHot(kind) {
  const url =
    DOUBAN +
    "?type=" +
    kind +
    "&tag=%E7%83%AD%E9%97%A8&sort=recommend&page_limit=25&page_start=0";
  const res = await withTimeout(
    fetch(url, {
      headers: {
        "User-Agent": UA,
        Referer: "https://movie.douban.com/",
        Accept: "application/json, text/plain, */*",
      },
    }),
    SEARCH_TIMEOUT
  );
  if (!res.ok) throw new Error("豆瓣返回 " + res.status);
  const json = await res.json();
  const out = [];
  for (const s of json.subjects || []) {
    if (!s || !s.title) continue;
    out.push({
      id: "douban|" + kind + "|" + encodeURIComponent(s.title),
      name: s.title,
      poster: s.cover || "",
      overview: s.rate ? "豆瓣评分 " + s.rate : "",
    });
  }
  return out;
}

async function sourceOf(key) {
  for (const s of await loadSources()) {
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

// plain strips the HTML a collection site writes its description in: the
// tags, the spaces it pads with, and the entities it escapes with.
function plain(html) {
  return String(html || "")
    .replace(/<[^>]*>/g, "")
    .replace(/&nbsp;/g, " ")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/\s+/g, " ")
    .trim();
}

function toVideo(key, vod) {
  return {
    id: encodeId(key, vod.vod_id),
    name: vod.vod_name || String(vod.vod_id),
    poster: vod.vod_pic || "",
    overview: plain(vod.vod_content) || vod.vod_remarks || "",
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
  const out = [];
  await Promise.allSettled([
    doubanHot("movie").then((v) => {
      if (v.length) out.push({ title: "豆瓣热门电影", videos: v });
    }),
    doubanHot("tv").then((v) => {
      if (v.length) out.push({ title: "豆瓣热门剧集", videos: v });
    }),
  ]);
  return { title: "LibreTV", sections: out };
}

export async function search({ query, offset }) {
  if (!query) return { videos: [] };
  const page = Math.floor((offset || 0) / PER_PAGE) + 1;
  const SOURCES = await loadSources();
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
  if (key === "douban") {
    // A Douban card: the parts after the kind carry the title, which the
    // collection sites answer for. The first hit plays.
    const parts = String(id || "").split("|");
    const name = decodeURIComponent(parts.slice(2).join("|") || "");
    if (!name) throw new Error("这个条目没有名字");
    const sr = await search({ query: name, offset: 0 });
    if (!sr.videos.length) throw new Error("各源都没有「" + name + "」");
    return title({ id: sr.videos[0].id });
  }
  const source = await sourceOf(key);
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
      overview: plain(vod.vod_content) || vod.vod_remarks || "",
    },
    episodes,
  };
}

// The play of a Douban card goes the same way its title does: the first
// source that has the title answers with its first episode.
export async function play({ id }) {
  const { key } = decodeId(id);
  if (key === "douban") {
    const parts = String(id || "").split("|");
    const name = decodeURIComponent(parts.slice(2).join("|") || "");
    if (!name) throw new Error("这个条目没有名字");
    const sr = await search({ query: name, offset: 0 });
    if (!sr.videos.length) throw new Error("各源都没有「" + name + "」");
    return play({ id: sr.videos[0].id + "|0" });
  }
  const { vodId, ep } = decodeId(id);
  const source = await sourceOf(key);
  if (!source) throw new Error("未知来源 " + key);
  const index = ep === undefined || ep === "" ? 0 : Number(ep);
  const json = await fetchJSON(source.api, "ids=" + encodeURIComponent(vodId));
  const vod = (json.list || [])[0];
  if (!vod) throw new Error("找不到这部影片");
  const eps = bestSource(vod) || [];
  const pick = eps[index] || eps[0];
  if (!pick) throw new Error("这一集没有播放地址");
  return stream(String(pick.url));
}
