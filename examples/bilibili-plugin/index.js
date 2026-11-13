// 哔哩哔哩：热门、搜索、分 P 和直链播放。Cookie 填在设置里。
const API = "https://api.bilibili.com";
const REFERER = "https://www.bilibili.com";
const UA =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36";

function cookie() {
  return (host.cookie() || host.setting("cookie") || "").trim();
}

function headers() {
  const h = { "User-Agent": UA, Referer: REFERER };
  const c = cookie();
  if (c) h.Cookie = c;
  return h;
}

async function api(path) {
  const res = await fetch(API + path, { headers: headers() });
  if (!res.ok) throw new Error("B 站返回 " + res.status);
  const body = await res.json();
  if (body.code !== 0) throw new Error(body.message || "B 站接口出错");
  return body.data;
}

function pic(url) {
  if (!url) return "";
  if (url.startsWith("//")) return "https:" + url;
  return url.replace(/^http:/, "https:");
}

function ms(d) {
  if (typeof d === "number") return d * 1000;
  const parts = String(d || "0").split(":").map(Number);
  let s = 0;
  for (const p of parts) s = s * 60 + (p || 0);
  return s * 1000;
}

const MIXIN = [
  46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49, 33,
  9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55, 40, 61, 26,
  17, 0, 1, 60, 51, 30, 4, 22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36, 20,
  34, 44, 52,
];

let wbi = null;

async function wbiKey() {
  const now = Date.now();
  if (wbi && now - wbi.at < 1800000) return wbi.key;
  const nav = await api("/x/web-interface/nav");
  const img = nav.wbi_img.img_url.split("/").pop().split(".")[0];
  const sub = nav.wbi_img.sub_url.split("/").pop().split(".")[0];
  const raw = img + sub;
  const key = MIXIN.map((i) => raw[i]).join("").slice(0, 32);
  wbi = { key, at: now };
  return key;
}

async function signed(params) {
  const key = await wbiKey();
  const all = Object.assign({}, params, { wts: Math.floor(Date.now() / 1000) });
  const q = Object.keys(all)
    .sort()
    .map(
      (k) =>
        encodeURIComponent(k) +
        "=" +
        encodeURIComponent(String(all[k]).replace(/[!'()*]/g, "")),
    )
    .join("&");
  return q + "&w_rid=" + host.crypto.md5(q + key);
}

function video(v) {
  return {
    id: v.bvid,
    name: String(v.title || "").replace(/<[^>]+>/g, ""),
    poster: pic(v.pic),
    overview: String(v.description || "").replace(/<[^>]+>/g, ""),
    duration: ms(v.duration),
    url: "https://www.bilibili.com/video/" + v.bvid,
  };
}

export async function home() {
  const d = await api("/x/web-interface/popular?ps=20&pn=1");
  return {
    title: "哔哩哔哩",
    sections: [{ title: "热门", videos: (d.list || []).map(video) }],
  };
}

export async function search({ query, offset }) {
  const q = await signed({
    search_type: "video",
    keyword: query,
    page: Math.floor((offset || 0) / 20) + 1,
  });
  const d = await api("/x/web-interface/wbi/search/type?" + q);
  const list = d.result || [];
  return { videos: list.map(video), more: list.length >= 20 };
}

export async function title({ id }) {
  const bvid = String(id).split("#")[0];
  const d = await api("/x/web-interface/view?bvid=" + bvid);
  const first = d.pages && d.pages.length ? d.pages[0].cid : d.cid;
  const episodes = (d.pages || []).map((pg, i) => ({
    id: bvid + "#" + pg.cid,
    name: pg.part || "P" + (i + 1),
    poster: pic(d.pic),
    duration: (pg.duration || 0) * 1000,
    series: d.title,
    episode: i + 1,
  }));
  return {
    video: {
      id: bvid + "#" + first,
      name: d.title,
      poster: pic(d.pic),
      overview: d.desc || "",
      duration: (d.duration || 0) * 1000,
      url: "https://www.bilibili.com/video/" + bvid,
    },
    episodes,
  };
}

export async function play({ id, quality }) {
  const raw = String(id);
  if (raw.startsWith("season:")) {
    return { open: "https://www.bilibili.com/bangumi/play/ss" + raw.slice(7) };
  }
  const [bvid, cid0] = raw.split("#");
  let cid = cid0;
  if (!cid) {
    const d = await api("/x/web-interface/view?bvid=" + bvid);
    cid = d.cid;
  }
  const base =
    "/x/player/playurl?bvid=" + bvid + "&cid=" + cid + "&fnver=0&fourk=1";

  // 1080P keeps picture and sound apart, as DASH does; 720P is one mp4.
  if (quality !== "720") {
    try {
      const d = await api(base + "&qn=80&fnval=16");
      const dash = d && d.dash;
      if (dash && dash.video && dash.video.length) {
        const v = dash.video.find((x) => x.id === 80) || dash.video[0];
        const a = dash.audio && dash.audio[0];
        return {
          type: "dash",
          dash: {
            video: v.baseUrl || v.base_url,
            audio: a ? a.baseUrl || a.base_url : "",
          },
          headers: playHeaders(),
          quality: v.id >= 80 ? "1080P" : v.id >= 64 ? "720P" : v.id + "P",
        };
      }
    } catch (e) {
      // Fall back to the mp4 below.
    }
  }

  const d = await api(base + "&qn=64&fnval=1&platform=html5&high_quality=1");
  const seg = d.durl && d.durl[0];
  if (!seg || !seg.url) throw new Error("没有可播放的地址");
  return {
    url: seg.url,
    type: "mp4",
    headers: playHeaders(),
    quality: "720P",
    duration: seg.length || 0,
  };
}

function playHeaders() {
  const h = { Referer: REFERER, "User-Agent": UA };
  const c = cookie();
  if (c) h.Cookie = c;
  return h;
}

export async function danmaku({ id }) {
  const [bvid, cid0] = String(id).split("#");
  let cid = cid0;
  if (!cid) {
    const d = await api("/x/web-interface/view?bvid=" + bvid);
    cid = d.cid;
  }
  const res = await fetch("https://api.bilibili.com/x/v1/dm/list.so?oid=" + cid, {
    headers: Object.assign({ "Accept-Encoding": "identity" }, headers()),
  });
  const xml = await res.text();
  const cues = [];
  const re = /<d p="([^"]+)">([^<]*)<\/d>/g;
  let m;
  while ((m = re.exec(xml))) {
    const parts = m[1].split(",");
    cues.push({
      time: Math.round(parseFloat(parts[0]) * 1000),
      mode: Number(parts[1]),
      color: "#" + Number(parts[3]).toString(16).padStart(6, "0"),
      text: m[2]
        .replace(/&amp;/g, "&")
        .replace(/&lt;/g, "<")
        .replace(/&gt;/g, ">")
        .replace(/&quot;/g, '"')
        .replace(/&#39;/g, "'"),
    });
  }
  cues.sort((a, b) => a.time - b.time);
  return { cues };
}

export async function account() {
  if (!cookie()) return null;
  const d = await api("/x/web-interface/nav");
  if (!d.isLogin) return null;
  return { id: String(d.mid), name: d.uname, avatarUrl: pic(d.face) };
}

async function mid() {
  const d = await api("/x/web-interface/nav");
  if (!d.isLogin) throw new Error("需要先登录");
  return String(d.mid);
}

export async function history() {
  const d = await api("/x/web-interface/history/cursor?ps=30&max=0");
  const videos = (d.list || [])
    .filter((v) => v.history && v.history.bvid)
    .map((v) => ({
      id: v.history.bvid + "#" + (v.history.cid || ""),
      name: v.title,
      poster: pic(v.cover),
      overview: v.author_name ? "UP主：" + v.author_name : "",
      duration: (v.duration || 0) * 1000,
      url: "https://www.bilibili.com/video/" + v.history.bvid,
    }));
  return { title: "历史", sections: [{ title: "最近看过", videos }] };
}

export async function favorites() {
  const uid = await mid();
  const folders = await api("/x/v3/fav/folder/created/list-all?up_mid=" + uid);
  const first = folders.list && folders.list[0];
  if (!first) return { title: "收藏", sections: [] };
  const d = await api(
    "/x/v3/fav/resource/list?media_id=" + first.id + "&ps=30&pn=1",
  );
  const videos = (d.medias || []).map((v) => ({
    id: v.bvid,
    name: v.title,
    poster: pic(v.cover),
    overview: v.intro || (v.upper && v.upper.name ? "UP主：" + v.upper.name : ""),
    duration: (v.duration || 0) * 1000,
    url: "https://www.bilibili.com/video/" + v.bvid,
  }));
  return { title: "收藏", sections: [{ title: first.title, videos }] };
}

export async function watchLater() {
  const d = await api("/x/v2/history/toview?ps=30&pn=1");
  const videos = (d.list || []).map((v) => ({
    id: v.bvid,
    name: v.title,
    poster: pic(v.pic || v.cover),
    overview: v.owner && v.owner.name ? "UP主：" + v.owner.name : "",
    duration: (v.duration || 0) * 1000,
    url: "https://www.bilibili.com/video/" + v.bvid,
  }));
  return { title: "稍后再看", sections: [{ title: "", videos }] };
}

export async function following() {
  const uid = await mid();
  const d = await api(
    "/x/space/bangumi/follow/list?type=1&follow_status=0&pn=1&ps=30&vmid=" + uid,
  );
  const videos = (d.list || []).map((s) => ({
    id: "season:" + s.season_id,
    name: s.title,
    poster: pic(s.cover),
    overview: s.total_count ? "全 " + s.total_count + " 集" : "",
    url: "https://www.bilibili.com/bangumi/play/ss" + s.season_id,
  }));
  return { title: "追番", sections: [{ title: "", videos }] };
}
