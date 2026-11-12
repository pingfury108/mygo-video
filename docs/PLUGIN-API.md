# MyGo Video 插件协议 v1（草案）

沿用 mygo-music 的目录式插件：`plugins/<id>/plugin.json` + 一个 `.js`/`.ts` 模块。app 用 moejs 运行，模块导出的函数名即能力名；没导出的能力，UI 自动隐藏。

与音乐版相比，视频版新增：`title`/`series`/`episodes`/`play`/`subtitles`/`danmaku`/`progress`/`qualities`，并把"可播放"拆成"直链（L1）"与"唤起（L2）"两种模式。

## 1. plugin.json

```json
{
  "id": "tencent",
  "name": "腾讯视频",
  "version": "0.1.0",
  "apiVersion": 1,
  "minAppVersion": "0.1.0",
  "main": "index.js",
  "kind": "video",
  "playback": "catalog",
  "permissions": {
    "network": ["*.qq.com", "*.gtimg.com", "*.video.qq.com"],
    "process": ["yt-dlp"],
    "webSignIn": true
  },
  "requestHeaders": [
    { "hosts": ["*.qq.com"], "kinds": ["image"], "headers": { "referer": "https://v.qq.com/" } }
  ],
  "settings": [
    { "key": "quality", "label": "默认清晰度", "type": "select",
      "options": [
        { "value": "1080", "label": "1080P" },
        { "value": "720", "label": "720P" }
      ] }
  ],
  "navigation": [
    { "builtin": "home" },
    { "title": "电视剧", "page": "tv", "icon": "tv" },
    { "title": "电影", "page": "movie", "icon": "film" },
    { "builtin": "following" }
  ],
  "contentRating": "official"
}
```

### playback 模式

| 值 | 含义 | app 行为 |
| --- | --- | --- |
| `stream` | 插件能给直链（L1） | 经代理播放 |
| `catalog` | 只给目录，播放靠唤起（L2） | 详情页"播放"打开官方深链/内嵌页 |
| `external` | 播放交给外部程序（L3） | 通过 `host.spawn` 启动解析器，或打开用户填的解析服务 |

`contentRating` 用于商店提示：`official`（官方源）、`community`、`local`。

## 2. 导出函数

每个函数接收一个对象，返回一个值或 Promise。超时 30s；`signIn` 5min。

### 账号

| 导出 | 参数 | 返回 |
| --- | --- | --- |
| `signIn` | | `{id, name, avatarUrl, vip?}` |
| `signOut` | | |
| `account` | | 上面的对象或 `null` |

登录首选 `host.webSignIn({url, cookiesOf, until, userAgent})`，拿到 Cookie 头存进 `host.storage`。

### 浏览 / 搜索

| 导出 | 参数 | 返回 |
| --- | --- | --- |
| `home` | | `Browse` |
| `browse` | `{page}` | `Browse` |
| `search` | `{query, kind, offset}`，kind = `titles`/`episodes`/`people` | `{titles?, episodes?, people?, more}` |
| `suggest` | `{query}` | `[string]` |
| `title` | `{id}` | `Title`（含 seasons/episodes 或单集） |
| `episodes` | `{id, season, offset}` | `{episodes, more}` |
| `following` | `{offset}` | `{titles, more}` |
| `history` | `{offset}` | `{titles, more}` |

`Browse` 与音乐版同构（`{title, subtitle, imageUrl, description, sections}`），section 可含 `titles`、`episodes`、`people`、`links`、`morePage`、`empty`。

### 播放

| 导出 | 参数 | 返回 |
| --- | --- | --- |
| `play` | `{id, quality}`，id 是 episode id | `Playback` |
| `qualities` | `{id}` | `[{id, label, width, height, bitrate}]` |
| `subtitles` | `{id}` | `[{lang, label, url?, cues?}]` |
| `danmaku` | `{id, from, to}` | `{comments} \| {url}` |
| `open` | `{id}` | `{url}`：L2 的官方播放页/深链 |
| `progress` | `{id, position, duration, done}` | |
| `resume` | `{id}` | `{position, duration}` 或 `null` |
| `like` / `follow` | `{id, on}` | |

`Playback`：

```ts
type Playback = {
  url: string
  type: "hls" | "mp4" | "dash"
  headers?: Record<string, string>   // Referer / User-Agent / Cookie / Origin
  keys?: { uri?: string; key?: string; iv?: string }[]  // HLS AES-128
  qualities?: { id: string; label: string; url: string }[]
  duration?: number                  // ms
  live?: boolean
  drm?: string                       // 非空：app 显示"需在官方 App 播放"
  subtitles?: SubtitleTrack[]
  danmaku?: { url?: string; format?: "xml" | "json" }
  poster?: string
  startAt?: number                   // ms，广告/片头偏移
}
```

`drm` 的约定：插件知道自己拿到的流受 DRM 保护时，**不要**返回伪装直链，而是返回 `{drm: "widevine"}` 或抛 `{code: "unavailable", message: "此内容需要在官方 App 中播放"}`。app 会给出"打开官方播放"按钮。

### 收藏 / 追剧

`saveTitle({id, on})`、`saveEpisode`、`isSaved({ids})`，以及可选 `syncProgress({items})`。

## 3. 类型

```ts
type Title = {
  id: string
  kind: "movie" | "series" | "anime" | "video" | "live"
  name: string
  originalName?: string
  year?: number
  rating?: number
  genres?: string[]
  overview?: string
  poster?: string        // 支持 {size} 占位
  backdrop?: string
  logo?: string
  episodeCount?: number
  seasons?: Season[]
  url?: string           // 官方页面
  saved?: boolean
  progress?: Progress
}

type Season = { number: number; name?: string; episodes: Episode[] }

type Episode = {
  id: string
  title: string
  season: number
  number: number
  duration?: number      // ms
  airDate?: string
  still?: string
  progress?: Progress
  unavailable?: boolean
}

type Progress = { position: number; duration: number; done?: boolean }

type Person = { id: string; name: string; role?: string; avatar?: string }
type Comment = { time: number; text: string; color?: string; mode?: "scroll" | "top" | "bottom"; size?: number }
```

## 4. 错误语义

```js
throw Object.assign(new Error("请先登录"), { code: "signIn" })
```

| code | app 行为 |
| --- | --- |
| `signIn` | 打开插件登录流程 |
| `setup` | 跳到该插件的设置页 |
| `unavailable` | 显示"不可用"与原因（可带 `openURL`） |
| `unsupported` | 隐藏该入口 |
| `geo` | "此内容在当前地区不可用" |
| `drm` | 显示"需在官方 App 播放" + 打开按钮 |

## 5. 能力协商

`Can(name)` 在编译期由导出表得出。导航项、按钮、页签据此隐藏。v1 能力名：

```
signIn signOut account
home browse search suggest
title episodes following history
play qualities subtitles danmaku open progress resume
saveTitle isSaved like follow syncProgress
```

## 6. 权限（在音乐版基础上扩展）

| 权限 | 说明 |
| --- | --- |
| `network` | fetch 可达的 host 白名单 |
| `process` | 可启动程序白名单（需用户授权） |
| `webSignIn` | 是否允许内嵌网页登录 |
| `requestHeaders` | 给 app 自己发的图片/视频请求注入头 |

**新增建议**：`playback: { allowLocal: true }` —— 允许插件让 app 访问 `127.0.0.1`（对接本机解析器/yt-dlp 服务）。默认关闭，需用户授权。

## 7. 示例（L1：通用 HLS 源）

```js
// index.js
const UA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15"

export async function search({ query, offset = 0 }) {
  const res = await fetch(`https://api.example.com/v1/search?q=${encodeURIComponent(query)}&from=${offset}`, {
    headers: { "user-agent": UA, authorization: `Bearer ${host.storage.get("token") || ""}` },
  })
  if (res.status === 401) throw Object.assign(new Error("请先登录"), { code: "signIn" })
  const data = await res.json()
  return { titles: data.items.map(toTitle), more: data.hasMore }
}

export async function play({ id, quality }) {
  const res = await fetch(`https://api.example.com/v1/play/${id}?q=${quality || "1080"}`)
  const s = await res.json()
  if (s.drm) return { drm: s.drm }
  return {
    url: s.url,
    type: s.url.includes(".m3u8") ? "hls" : "mp4",
    headers: { referer: "https://example.com/", "user-agent": UA },
    duration: s.durationMs,
    subtitles: s.subs,
  }
}
```

## 8. 兼容与演进

- `apiVersion` 为整数，app 只加载 `<=` 自己支持的版本。
- 新字段只增不改；`play` 的返回保持向后兼容（旧 app 忽略未知字段）。
- 插件可声明 `experimental: true`，设置页标黄提示。
