# MyGo Video 设计方案

> 目标：以 [mygo-music](https://github.com/egoist/mygo-music) 为参照，用 [MyGo](https://github.com/egoist/mygo) **v0.9.0** 做一个原生视频播放器，通过插件接入腾讯视频、优酷等站点。
>
> 本文是"学习 + 方案"，不含业务代码。插件协议细则见 [PLUGIN-API.md](PLUGIN-API.md)，界面稿见 `designs/mygo-video/`。

## 0. 一句话结论

- **播放内核可行**：MyGo v0.9.0 自带 `ui/video`，用系统媒体框架（AVFoundation / Media Foundation / GStreamer）硬解视频、播声音、走 GPU 零拷贝，原生支持 HLS。视频版不需要自己写解码器。
- **插件架构可行**：mygo-music 已经验证了"Go 主程序 + 目录式 JS/TS 插件 + 权限声明 + 能力协商 + i18n + 安装/更新"这一整套模式，视频版可以沿用并把 `music.Source` 换成 `media.Source`。
- **腾讯/优酷正片不能靠"插件给个直链"解决**：会员正片普遍有私有签名、防盗链和 DRM，MyGo 也没有 DRM 支持。方案把它们定位为 **L2 适配器**（搜索/详情/收藏/续播 + 唤起官方播放或内嵌网页播放），直链播放只对免费/UGC/预告片等可用内容成立，并保留 L3 外部解析器接口。**不做 DRM 绕过。**
- **优先做能稳定播的源**：本地文件、WebDAV、Jellyfin/Emby、IPTV/m3u8、用户自备 yt-dlp 的站点 —— 这些是视频版真正的基本盘。

---

## 1. 学习：mygo-music 是怎么搭的

### 1.1 基本盘

| 项 | 值 |
| --- | --- |
| 语言/框架 | Go 1.27.3 + MyGo v0.9.0，纯 Go、无 cgo |
| UI | MyGo 原生 UI（GPU 绘制，无 WebView） |
| 规模 | `internal/` 约 4 万行 Go |
| 构建 | `go tool mygo dev` / `go tool mygo build`，`mygo.config.ts` 里写名称、bundle id、签名、公证、更新公钥 |
| 许可 | AGPL-3.0（MyGo 本体是 MIT，moejs 是 Apache-2.0） |

### 1.2 分层

```
main.go
└─ internal/app        应用状态、路由、所有视图（ui_*.go）
   ├─ internal/music   领域模型 + Source 接口（插件要实现的契约）
   ├─ internal/plugin  JS 运行时宿主（moejs），把插件包装成 music.Source
   ├─ internal/local   内置本地音源（扫描、标签、封面、歌词）
   ├─ internal/player  音频解码/流式播放/缓存/淡入淡出
   ├─ internal/lyrics  歌词解析与排版
   ├─ internal/speech  播客本地转写（sherpa-onnx，可选下载）
   └─ internal/i18n    中英文案表
```

值得抄的三条结构原则：

1. **领域包不认识 UI**：`music.Source` 只描述"能做什么"，插件和本地库都实现它，app 只面对接口。
2. **每个源一个 `library` 缓存**：`sources.go` 的 `library` 保存账号、首页、播放列表、详情、搜索的返回；异步结果落到发起请求的那个源，切源不串数据。
3. **主线程单写**：所有视图状态只在主线程改；goroutine 通过 `post` 回主线程。

### 1.3 插件系统（最值得学的部分）

- **形态**：`plugins/<id>/` 目录 = `plugin.json` + 一个 `.js`/`.ts` 模块（`moejs` 编译，支持 TS），**无需构建、跨平台同一份代码**。
- **契约**：模块导出函数，函数名就是 `music.Source` 的方法名（`search`/`stream`/`playlist`…）。没导出的能力视为不支持，UI 自动隐藏。
- **沙箱**：每个插件一个 JS 运行时；内存上限 256MB，单次调用 30s 超时，卡死 1s 判定并重启；崩溃不影响主程序。
- **权限**：`network`（host 白名单，`*` 全开）、`process`（可执行程序白名单，需用户二次授权）、`requestHeaders`（给图片/音频请求注入 Referer/Cookie 等）。
- **host API**：`storage`、`setting`、`fetch`、`crypto.md5/aesEcbEncrypt`、`openURL`、`signInPrompt`（二维码）、`waitForRedirect`（本地回调登录）、`webSignIn`（内嵌网页登录拿 Cookie）、`spawn`（起外部程序）、`t`/`language`。
- **能力协商**：`Can(name)` 在编译期就知道导出哪些函数，首帧即可隐藏不支持的入口。
- **错误语义**：抛 `{code}` = `signIn` / `setup` / `unavailable` / `unsupported`，app 据此弹登录、跳设置、显示不可用。
- **分发**：GitHub 仓库 + `mygo-music-plugin` topic；release 里放平台 zip 或直接取源码；`mygo-music://install-plugin?repo=owner/repo` 一键安装；`minAppVersion` 控制兼容。
- **i18n**：`locales/zh-CN.json` 翻译插件自己的文案。

### 1.4 UI 结构（可直接沿用的骨架）

- 左 **232 DIP 侧边栏**：返回/前进、搜索、源切换器、导航项、我的歌单。
- 中间内容区：每页一个虚拟化列表（`ui.ListState`），顶部浮动工具栏，滚动到边缘有玻璃渐隐。
- 底部 **84 DIP 播放条**：封面/标题/喜欢 | 上一首/播放/下一首 | 歌词/队列/音量/进度。
- **Now Playing 全屏层**：从播放条封面"长出来"，含舞台（封面/歌词/队列/评论）四栏、⌘1–4 切栏、Esc 关闭。
- 设置是**独立窗口**，含通用/外观/插件/实验/关于；插件页支持 + 安装、⋯ 卸载、权限查看、设置项（text/password/boolean/select）。
- 托盘、macOS 刘海播放器、媒体键、恢复上次会话、深浅色、系统强调色（每个源/封面提取 accent）。

### 1.5 视频版要改的地方

| mygo-music | mygo-video |
| --- | --- |
| `music.Track/Album/Artist` | `media.Title/Series/Season/Episode/Person` |
| `music.Stream`（音频流） | `media.Playback`（HLS/MP4 + headers + 字幕 + 弹幕 + DRM 标记） |
| `player` 自己解码音频 | `ui/video` 系统解码，播放器只做状态机与阀门 |
| 歌词（LRC/逐字） | 字幕（SRT/VTT/ASS）+ 弹幕（滚动/顶部/底部轨道） |
| Now Playing 音乐舞台 | 全屏播放页：视频层 + 控制层 + 弹幕层 + 剧集抽屉 |
| 播放条 84 DIP | 迷你播放条（继续观看）+ 全屏播放页 |
| — | 本地视频库扫描/刮削、WebDAV、IPTV |

---

## 2. MyGo v0.9.0 能给我们什么

### 2.1 视频（已确认）

```go
import "github.com/egoist/mygo/ui/video"

p, err := video.OpenURL("http://127.0.0.1:PORT/s/abc.m3u8")
ui.Video(c, p).Fill().Fit(ui.Contain)
```

- 系统解码：macOS AVFoundation、Windows Media Foundation、Linux GStreamer；浏览器里是 `<video>`。
- 格式：H.264 + AAC（MP4/MOV）全平台；HEVC/VP9/AV1 看系统；**HLS 在 macOS/Windows/浏览器可用**。
- 帧走 GPU（iOSurface/纹理），窗口每帧只取"该显示的那张"。
- `VideoPlayer` 有 `Play/Pause/Seek/SetRate/SetVolume/SetMuted/SetLoop`、`Position/Duration/Size/Ready/Buffering/Ended/Err`。
- **限制（影响方案）**：
  1. `OpenURL` **不能带自定义请求头**（没有 Referer/Cookie 参数）→ 必须走本地代理。
  2. 无 DRM（无 Widevine/FairPlay）→ 会员正片播不了。
  3. Linux 依赖系统 GStreamer 1.x + base 插件，打包要说明。
  4. 没有内置画中画（PiP 只在 WebView 页面里有）；原生 Playground 需自己做"置顶小窗"。

### 2.2 其它可用的原生能力

- `ui.Draw/DrawOver` + `Painter.Text/Image/Path`：弹幕、字幕、进度条预览、自定义控件。
- `ui.Canvas`、`shaders`：封面模糊、氛围光、转场。
- `AnimateTo`/`After`/`Motion`：播放页进出、控制条浮现、弹幕滚动。
- 虚拟化列表/网格、无障碍、上下文菜单、toast、dialog、快捷键。
- `mygo.Window`：多窗口（设置、内嵌登录页、独立播放窗）。
- 托盘、媒体键、单实例、深链（`mygo-video://`）、自动更新。

### 2.3 与插件的关系

MyGo 自己有一套"原生模块插件"（Go 插件，如 glass），**和 mygo-music 的 JS 源插件不是一回事**。视频版要做的是后者：app 内自建插件宿主（直接借 mygo-music 的 `internal/plugin` 设计）。

---

## 3. 腾讯视频 / 优酷：可行性判断

### 3.1 内容分层

| 内容 | 典型情况 | 直链播放 |
| --- | --- | --- |
| 预告、花絮、部分 UGC/短视频 | 免费、可能有简单防盗链 | 可行（需 Referer/Cookie + HLS 解密） |
| 免费正片（少量） | 免费但有 CDN 鉴权、时效 token | 通常可行，但易失效 |
| 会员/付费正片 | 会员鉴权 + DRM（Widevine 等）+ 私有签名 API | **不可行**（无 DRM 支持，也不应绕过） |
| 直播 | 私有协议/加密 | 一般不保证 |

### 3.2 适配器分级（产品上的诚实设计）

- **L1 直链播放**：插件返回 `{url, type, headers, key?}`，app 本地代理注入头、解 HLS AES-128，交给 `ui/video`。
- **L2 目录 + 唤起**：插件提供搜索、详情、分集、收藏/追剧、进度同步；播放按钮打开 **官方 App/网页深链** 或**内嵌 WebView**。进度仍可在 app 里记录。
- **L3 外部解析器**：插件设置里填"解析服务地址"或授权 `host.spawn` 调用用户自己的 yt-dlp / 解析器；app 只负责播放返回的流。**不内置任何破解/绕过 DRM 的代码。**

> 建议：腾讯/优酷首发就按 **L2** 做（体验完整、稳定、合规），把 L1 留给免费内容和用户自备的源；在插件卡片上明示能力等级，避免"装上了却播不了"的失望。

### 3.3 合规

- 不内置 DRM 绕过、不内置解密密钥抓取。
- 插件生态由用户自行安装，第三方插件行为与 app 无关；插件市场做基本审核与下架机制。
- 保留"仅播放用户有权访问的内容"的提示（首次安装平台插件时）。

---

## 4. 总体架构

```
┌────────────────────────────── app（主线程 UI）──────────────────────────────┐
│  ui_main     侧边栏 / 内容区 / 迷你播放条 / 路由 / 快捷键                      │
│  ui_home     首页：继续观看、追剧、推荐、最近添加                              │
│  ui_detail   影视详情：海报、元信息、分集、简介、相关                          │
│  ui_player   全屏播放：视频层 + 控制层 + 弹幕层 + 字幕层 + 剧集抽屉            │
│  ui_search / ui_library / ui_settings / ui_sources                            │
└───────────────┬───────────────────────────────────────────────┬──────────────┘
                │                                               │
        ┌───────▼────────┐                              ┌───────▼────────┐
        │ media (领域)    │                              │ store (持久化)  │
        │ Title/Series    │                              │ 进度/收藏/设置   │
        │ Episode/Playback│                              │ JSON + 原子写    │
        └───────┬────────┘                              └────────────────┘
                │ Source 接口
   ┌────────────┼──────────────┬─────────────────┬──────────────────┐
   ▼            ▼              ▼                 ▼                  ▼
local(内置)   plugin(JS)     webdav           jellyfin           iptv
扫描/刮削      腾讯/优酷/     WebDAV 浏览       REST API           m3u8 列表
              Bilibili/...   直链播放          直链播放            直链播放
                │
                ▼
        ┌────────────────┐      ┌──────────────────────────┐
        │ streamproxy    │─────▶│ ui/video（系统解码/GPU）   │
        │ 本机 HTTP 代理  │      └──────────────────────────┘
        │ 注入头/Range/   │
        │ HLS 重写/解密   │
        └────────────────┘
                ▲
        ┌───────┴────────┐
        │ subtitle 字幕   │  danmaku 弹幕（轨道/渲染）
        └────────────────┘
```

### 4.1 目录结构

```
mygo-video/
├── main.go
├── mygo.config.ts            # name/identifier/版本/深链/更新/签名
├── go.mod                    # go 1.27.3 + github.com/egoist/mygo v0.9.0
├── internal/
│   ├── media/                # 领域模型 + Source 接口 + 错误语义
│   ├── plugin/               # JS 宿主（manifest、vm、host API、安装）
│   ├── streamproxy/          # 本地代理 + HLS 重写/解密 + header 注入
│   ├── playback/             # 播放器状态机、阀门、续播、快捷键
│   ├── subtitle/             # SRT/VTT/ASS 解析与轨道
│   ├── danmaku/              # 弹幕模型、拉取、轨道调度、绘制
│   ├── local/                # 本地视频库（扫描、nfo/海报、时长）
│   ├── sources/              # webdav/jellyfin/iptv 内置源
│   ├── store/                # 进度、追剧、设置、搜索历史（JSON）
│   ├── app/                  # 全部 ui_*.go
│   └── i18n/
├── designs/mygo-video/       # 本方案的界面稿
└── docs/
```

### 4.2 领域模型（要点）

```go
type Title struct {          // 电影 / 剧集 / 番剧 / 视频 / 直播
    ID, Source, Name string
    Kind Kind                // movie | series | anime | video | live
    Poster, Backdrop, Logo string
    Year int; Rating float64; Genres []string; Overview string
    Seasons []Season; EpisodeCount int
    Status Status            // 未看/在看/看完/追剧中
    Progress Progress        // 上次看到哪
}

type Episode struct {
    ID, Title string
    Season, Number int
    Duration time.Duration
    AirDate string
    Progress Progress
}

type Playback struct {
    URL     string
    Type    string            // hls | mp4 | dash
    Headers map[string]string // Referer/UA/Cookie…
    Keys    []HLSAESKey       // HLS AES-128（可选，插件给）
    Qualities []Quality       // 可选清晰度
    Subtitles []Subtitle      // 外挂/内嵌
    Danmaku   *DanmakuSource
    Live      bool
    DRM       string          // 非空 = 需要 DRM，app 直接显示"此内容需在官方 App 播放"
}
```

### 4.3 关键决策

| # | 决策 | 理由 |
| --- | --- | --- |
| D1 | 播放走 `ui/video` + 本地 HTTP 代理 | 唯一能带 Referer/Cookie 且能解 HLS AES-128 的方式；不动 MyGo 源码 |
| D2 | 插件返回"意图"而非"播放器对象" | 插件只描述流；代理、解密、播放、进度都由 app 统一控制 |
| D3 | 腾讯/优酷按 L2 首发 | DRM 不可绕过；目录+唤起已能提供 80% 的"聚合"价值 |
| D4 | 本地源是一等公民 | 保证产品在没有任何平台插件时可用（离线、隐私） |
| D5 | 进度/收藏存本地 JSON，插件可选同步 | 简单、可调试；插件同步按能力开启 |
| D6 | 插件协议带 `apiVersion` + 能力位 | 视频领域比音乐复杂，必须能演进 |

---

## 5. 播放管线

### 5.1 流程

```
点击播放
  → playback.Open(episode)
      → source.Play({id, quality})          [插件，异步，可 30s]
      → Playback{url, headers, keys, subs, danmaku}
      → streamproxy.Register(playback)      [127.0.0.1:随机端口]
           ├─ 普通 mp4：透传 + Range + 头
           ├─ HLS：拉 m3u8 → 重写分片/密钥 URI → 代理地址
           │        AES-128：自己解，输出明文 TS/fMP4
           └─ 错误：401/403 → 让插件刷新 URL，重试一次
      → ui/video.OpenURL(proxyURL)
      → ui.Video 渲染；控制层/弹幕层/字幕层叠加
```

### 5.2 streamproxy 设计要点

- **只监听 127.0.0.1**，每次播放一个随机路径 token，播放结束即注销。
- **头注入**：`Referer`、`User-Agent`、`Origin`、`Cookie`（来自插件 `webSignIn` 或设置）。
- **HLS 重写**：解析 master/media playlist；分片 URI、`#EXT-X-KEY:URI`、`#EXT-X-MAP`、`#EXT-X-MEDIA` 全部改写到 `/s/<token>/...`；保留 `#EXT-X-BYTERANGE`。
- **AES-128**：密钥由插件给出（`keys: [{uri|key, iv}]`）或代理去取；在内存解密分片，不落盘。
- **Range**：MP4 拖动进度必需；透传上游 206。
- **超时与重试**：连接 10s，分片 30s；连续 3 次失败 → 播放器 `Err` → UI 给出"换清晰度/重试/用官方 App 打开"。
- **缓存**：可选 512MB 磁盘缓存（分片级 LRU），断网可续看。
- **日志**：只记 token 与状态码，不记 URL 里的敏感参数。

### 5.3 播放器状态机

`Loading → Ready ⇄ Buffering → Ended/Failed`，映射到 `ui.Video` 的 `Ready/Buffering/Ended/Err`。

- 进入播放页：恢复上次位置（> 5s 且 < 95% 时询问"继续观看 / 从头"）。
- 每 5s 或暂停/退出时写进度（本地）+ 可选 `source.Progress()`。
- 播完：标记剧集已看，自动下一集倒计时 8s（可关）。
- 键盘：Space/←→ 5s/↑↓ 音量/f 全屏/d 弹幕/m 静音/⌘←→ 上下集/Esc 退出。
- 控制条 3s 无操作淡出；鼠标移到顶部显示标题与返回。
- 画质切换：保留位置重建代理与播放器（<300ms）。

### 5.4 弹幕

- 拉取：`source.Danmaku({id, segment})` 按 6 分钟分段，或直接给 XML/JSON URL。
- 轨道算法：滚动（右→左，按速度分道）、顶部、底部；碰撞检测用"尾端时间"队列。
- 渲染：`DrawOver` 自绘（描边文字），只画可见时间窗 ±0.5s；上限 200 条/屏；可调透明度/字号/速度/屏蔽词。
- 时间基准用播放器 `Position()`，暂停即停。

### 5.5 字幕

- 解析 SRT/VTT/ASS（ASS 只取文本+基础样式），生成 `[]Cue{start,end,text}`。
- 外挂：URL 或本地文件；内嵌：暂不支持（系统引擎不暴露字幕轨），由插件提供外挂地址。
- 双语：两条轨道同时显示，主/副字号区分。
- 渲染：底部安全区，自动换行；可拖动位置、调字号、加背景。

---

## 6. UI / UX

### 6.1 信息架构

```
侧边栏
├─ 返回 / 前进
├─ 搜索（⌘F）
├─ 源切换器（本地 / 腾讯 / 优酷 / Bilibili / WebDAV / Jellyfin）
├─ 首页
├─ 继续观看
├─ 追剧
├─ 收藏
├─ 历史
└─ 设置（独立窗口）
```

页面：首页 / 详情 / 播放（全屏覆盖层）/ 搜索 / 库 / 源与插件设置。

### 6.2 视觉方向

**"影院黑 + 原生玻璃"**：沿用 mygo-music 的原生 macOS 质感（232 侧栏、玻璃渐隐、系统字体），但内容更暗、图像更大、强调色从当前影片海报提取。

- 底色 `#0E0F12`，内容层 `#15171C`，侧栏 `#101114`。
- 强调色：从海报取主色（OKLCH 提饱和），默认暖橙 `oklch(.7 .16 45)`。
- 字体：`-apple-system` + `PingFang SC`；标题 600，正文 13–14。
- 圆角：卡片 10，海报 8，播放器 12；间距 8 的倍数。
- 动效：播放页从缩略图/卡片放大进入（morph），控制条 200ms 浮现，弹幕匀速。

### 6.3 关键屏

1. **首页**：顶部"继续观看"大卡（16:9 剧照 + 进度 + 一键续播），下面"追剧"横排（海报 2:3，更新角标）、"为你推荐"、"最近添加"。
2. **详情**：全宽剧照背景 + 海报 + 标题/年份/评分/类型 + 播放/追剧/更多；分集按季分组，看过的显示进度；简介可展开；相关推荐。
3. **播放**：视频占满，控制条浮在底部（章节化进度条、时间、播放/上下集、弹幕、字幕、清晰度、倍速、剧集、全屏）；鼠标静止 3s 全隐；顶部返回 + 标题 + 源徽标。
4. **搜索**：大搜索框 + 源过滤 chips + 分组结果（影片/剧集/人物）+ 键盘上下选择。
5. **源与插件**：每个源一张卡，显示能力等级（L1 直链 / L2 唤起 / 本地）、账号状态、权限、设置、更新；+ 从 GitHub/文件夹安装。
6. **库**：追剧网格（更新优先）、收藏、历史（按时间分组）。

界面稿：`designs/mygo-video/index.html`（可点击切换 6 屏，含播放/弹幕/进度交互），设计说明见 `designs/mygo-video/DESIGN-NOTES.md`。

---

## 7. 里程碑

| 阶段 | 内容 | 验收 | 状态 |
| --- | --- | --- | --- |
| M0 骨架 | 项目初始化、`ui_main` 侧栏/路由、深色主题、i18n | 能跑起来，切页正常 | 已完成（界面与路由；i18n 暂未做） |
| M1 本地播放 | 本地库扫描、详情、`ui/video` 播放、续播、字幕、快捷键 | 拖入一个 mp4/mkv 能看，退出再进能续 | 已完成：扫描/详情/播放/续播/快捷键；字幕留到 M1.5 |
| M2 插件宿主 | manifest、moejs 运行时、host API、能力协商、设置/安装 | demo 插件可搜索并播放一条 HLS | 完成：宿主、文件夹/zip/GitHub 安装、设置、账号、海报 |
| M3 代理与 HLS | streamproxy、header 注入、m3u8 重写、AES-128、Range | 带 Referer 的 HLS 能播、能拖 | 完成：明文 HLS / AES-128 / Range，另有 DASH、FLV 转 HLS |
| M4 体验 | 弹幕、剧集连播、画质切换、迷你窗、托盘/媒体键 | 弹幕可开关调速；连播正常 | 完成：连播/选集/弹幕（含样式）/画质；迷你窗与媒体键未做 |
| M5 平台适配 | 腾讯/优酷 L2（搜索/详情/深链/内嵌 WebView）、WebDAV/Jellyfin/IPTV | 能搜到、能记录进度、能唤起播放 | WebDAV、IPTV、B 站完成；优酷搜到即可打开；腾讯只有入口（搜索接口需签名） |
| M6 发布 | 签名/公证/更新、插件安装页、文档 | 三平台安装包 | 部分：CI 四平台构建 + 打 tag 发 Release；签名与公证未做 |

---

## 8. 风险与待定

1. **腾讯/优酷的 L2 也需要登录态**：`host.webSignIn` 是唯一稳妥方式；接口可能随版本变化，插件需版本化。
2. **HLS 解密合法性**：只解用户有权访问的流；不提供密钥获取能力。
3. **Windows 的 HLS**：文档称支持，但需实测（Media Foundation 的 HLS 支持依赖系统版本）；必要时代理转封装。
4. **Linux 打包**：需声明 GStreamer 依赖；AppImage/deb 里加 `Recommends`。
5. **弹幕性能**：必须限流 + 只画可见；用 `perf_test.go` 思路设帧预算。
6. **许可证**：mygo-music 是 AGPL-3.0，**不要直接复制其源码**；本方案只借鉴架构，独立实现可按 MIT。
7. **画中画**：原生没有 PiP，M5 前用"置顶小窗 + 记住窗口位置"替代。

---

## 9. 下一步（等你确认）

1. 确认首发源范围：**本地 + WebDAV/Jellyfin + 一个平台（Bilibili 或 腾讯 L2）** 是否合适？
2. 确认插件协议命名与字段（`media.Source` / `play` / `danmaku`），再定 M2。
3. 确认视觉方向（影院黑）与首页信息优先级，我再把界面稿细化到组件级。
