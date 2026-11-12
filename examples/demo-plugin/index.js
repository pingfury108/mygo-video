// A source of public test streams: enough to try the plugin host, the
// proxy and the player with a real HLS stream. Copy this folder into the
// app's plugins directory and restart.

const STREAM =
  "https://devstreaming-cdn.apple.com/videos/streaming/examples/bipbop_4x3/bipbop_4x3_variant.m3u8";

const ACCOUNT = { id: "demo", name: "演示账号" };

export async function home() {
  return {
    title: "演示源",
    subtitle: "公开的 HLS 测试流",
    sections: [
      {
        title: "测试流",
        videos: [
          {
            id: "bipbop",
            name: "BipBop",
            poster: "https://picsum.photos/seed/bipbop/640/360",
            overview: "Apple 的公开 HLS 测试流，用来验证代理、HLS 重写和播放。",
          },
          {
            id: "bipbop-2",
            name: "BipBop（第二个）",
            poster: "https://picsum.photos/seed/bipbop2/640/360",
            overview: "同一个流，用来验证卡片与详情页。",
          },
        ],
      },
    ],
  };
}

export async function search({ query }) {
  const q = String(query || "").toLowerCase();
  const hit = q === "" || "bipbop".includes(q);
  return {
    videos: hit ? [{ id: "bipbop", name: "BipBop · 搜索 " + query }] : [],
    more: false,
  };
}

export async function title({ id }) {
  return { video: { id, name: "BipBop" }, episodes: [] };
}

export async function play() {
  return {
    url: STREAM,
    type: "hls",
    headers: { "user-agent": "MyGo Video" },
  };
}

export async function progress() {}

export function signIn() {
  host.storage.set("signedIn", true);
  return ACCOUNT;
}

export function signOut() {
  host.storage.remove("signedIn");
}

export function account() {
  return host.storage.get("signedIn") ? ACCOUNT : null;
}
