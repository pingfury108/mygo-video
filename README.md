# MyGo Video

用 Go 和 [MyGo](https://github.com/egoist/mygo) 写的原生桌面视频播放器。没有 WebView，界面由 MyGo 在 GPU 上绘制；视频交给系统的媒体框架硬解（macOS AVFoundation、Windows Media Foundation、Linux GStreamer）。

当前是 **M0 + M1**：本地视频库、详情、播放、续播、键盘控制。插件系统（腾讯/优酷等来源）按 [docs/PLAN.md](docs/PLAN.md) 的 M2–M5 逐步加。

## 运行

```bash
go tool mygo dev      # 开发运行，改 .go 自动重启
go tool mygo build    # 打包到 build/
```

## 启动

`go tool mygo dev` 每次改动都会重新编译并组装 `.app`：热缓存下 `go build` 约 3 秒，整包再多一点。**应用本身启动很轻**（2026-11-12 实测，`startup.log`）：

| 阶段 | 耗时 |
| --- | --- |
| 进程启动 → `WhenReady` | 202–236 ms |
| → 窗口创建 | 253–306 ms |
| → 扫描完成（3.2 万个文件） | 453–543 ms |

扫描在后台跑；第二次启动先用上次扫描的结果（`state.json` 里的 `items`），窗口一出现就有内容。开发版把每一步写进 `~/Library/Application Support/MyGo Video Dev/startup.log`。

只想看应用启动速度时，先用 `go tool mygo build` 打包，再直接打开 `build/` 里的 `.app`，就绕开了 dev 的编译。

## 图标

`resources/icon.png`（暖橙，正式版）和 `resources/icon-dev.png`（青色，开发版）由同目录的 `icon.svg` / `icon-dev.svg` 生成：1024×1024，透明边距按 macOS 图标栅格（824 内容区）。`mygo build` 会把它做成 macOS 的 `.icns`、Windows 的图标资源和 Linux 桌面图标。

首次启动会扫描：

- 应用数据目录下的 `videos/`（macOS：`~/Library/Application Support/dev.pingfury.video/videos`）
- `~/Movies`
- `~/Desktop`
- `~/Downloads`

把视频放进去，点右上角的重新扫描。

## 现在能做什么

- 本地视频库：扫描文件夹，按修改时间排序，识别 `S02E05` 这样的集数
- 内置 WebDAV：设置里填地址和账号，浏览服务器文件夹，播放视频
- 插件源：数据目录 `plugins/` 下的 JS/TS 插件，接入后出现在侧栏底部的源列表里；播放走本机代理
- 封面：用本机 `ffmpeg` 从视频 10 秒处抽帧，640×360 JPEG 缓存在数据目录；列表、详情海报、首页 hero 都用它，没有 ffmpeg 时回退到首字色块
- 首页：继续观看（全宽 hero）、最近添加
- 片库：网格
- 搜索：按文件名、剧名、集数过滤，`⌘F` 聚焦
- 详情：海报、文件信息、选集、播放进度、从头播放、清除记录
- 剧集：同名的 `S02E05` 归到同一部剧，详情页选集，播放页切上一集/下一集
- 播放：系统解码、进度条拖动、音量、10 秒快进/后退、续播、倍速、全屏
- 字幕：自动加载同名的 `.srt` / `.vtt`（含 `.zh.srt`），播放页显示，可开关
- 控制条：播放时 3 秒不动鼠标淡出，移动鼠标回来
- 快捷键：`空格` 播放/暂停，`← →` 后退/快进，`↑ ↓` 音量，`C` 字幕，`F` 全屏，`,` `.` 上一集/下一集，`Esc` 退出

格式支持取决于系统：MP4 / MOV（H.264 + AAC）在各平台都能播；MKV、WebM、AVI、HEVC 看系统有没有对应解码器。Linux 需要 GStreamer 1.x 及 base 插件。字幕只读外挂文件，不读内嵌字幕轨。

## 插件

插件是一个文件夹，放在应用数据目录下的 `plugins/<id>/`，里面是 `plugin.json` 和一个 JS/TS 模块：

```
~/Library/Application Support/MyGo Video Dev/plugins/demo/
  plugin.json
  index.js
```

`plugin.json` 声明 `id`、`name`、`apiVersion`、`main`、`playback` 和权限。模块导出函数，名字就是能力：

| 导出 | 参数 | 返回 |
| --- | --- | --- |
| `home` | | `{title, sections: [{title, videos: [...]}]}` |
| `browse` | `{page}` | 同上 |
| `search` | `{query, offset}` | `{videos, more}` |
| `title` | `{id}` | `{video, episodes}` |
| `play` | `{id}` | `{url, type, headers, open, duration, drm}` |
| `progress` | `{id, position, duration, done}` | |
| `signIn` / `signOut` / `account` | | 账号 |

`permissions.network` 同时限制 `fetch` 和本机代理能访问的 host。`play` 返回 `open` 时在浏览器打开；返回 `drm` 时应用明确拒绝播放。

复制 `examples/demo-plugin` 到数据目录的 `plugins/`，重启后侧栏底部就能切到「演示源」，播放 Apple 的公开 HLS 测试流。

设置页的「插件」里能做：

- **安装插件…**：从三种地方装
  - **文件夹**：选一个含 `plugin.json` 的文件夹
  - **zip**：解压后找 `plugin.json`，拒绝跑到目录外的路径
  - **GitHub**：填 `owner/repo` 或仓库地址，取最新 release 里适合本平台的 zip；没有 release 就用默认分支
- **设置项**：manifest 的 `settings` 变成输入框，改完即保存
- **账号**：`登录` / `退出`，调插件的 `signIn` / `signOut` / `account`
- **海报**：从 `poster` 字段加载网络图片，受 `permissions.network` 限制

## 内置源

WebDAV：设置 → 内置源，填地址（含路径，如 `https://nas.local:5005/dav`）、用户名和密码。保存后侧栏底部出现「WebDAV」，可以：

- 浏览文件夹，面包屑返回上级
- 播放里面的视频，走本机代理带上 Basic Auth
- 进度存在本地，和本地视频一样续播

## 结构

```
main.go
internal/
  app/          视图与状态：侧栏、首页、详情、播放、搜索、设置
  local/        本地库：扫描、文件名解析
  media/        领域模型：视频、进度、时长格式化
  plugin/       JS/TS 插件宿主：manifest、moejs、host API
  store/        进度、音量、扫描目录（JSON）
  subtitle/     SRT / VTT 解析
  thumb/        ffmpeg 抽帧，缩略图缓存
  streamproxy/  本机代理：请求头注入、HLS 重写、AES-128 解密
  webdav/       WebDAV 客户端：PROPFIND、Basic Auth
examples/
  demo-plugin/  一个公开 HLS 测试流的插件
docs/
  PLAN.md        学习笔记、架构、插件协议、里程碑
  PLUGIN-API.md  插件协议草案
designs/
  mygo-video/    界面设计稿（可交互 HTML）
```

## 设计

界面稿：`designs/mygo-video/index.html`，说明见 `designs/mygo-video/DESIGN-NOTES.md`。

预览：

```bash
python3 -m http.server 4311 --directory designs
# http://localhost:4311/mygo-video/index.html
```

## 已知限制

- 插件是首版：安装支持文件夹 / zip / GitHub 仓库，没有商店与签名校验；账号只做了 `signIn` / `signOut`，没有二维码与内嵌登录页
- 封面只是正片里的一帧，不是刮削来的海报；没有 `ffmpeg` 时回退到首字色块
- 字幕只读外挂的 `.srt` / `.vtt`，不读内嵌字幕轨，也不支持 ASS 样式
- 没有弹幕（等插件来源）
- 没有画中画（原生窗口没有 PiP）
- 受 DRM 保护的内容无法播放，这是有意为之

## 下一步

1. M2 插件宿主：`plugin.json`、moejs 运行时、host API、设置与安装
2. M3 本机代理：请求头注入、HLS 重写、AES-128、Range
3. M4 字幕与弹幕
4. M5 腾讯/优酷的 L2 适配器（目录 + 唤起官方播放）

见 [docs/PLAN.md](docs/PLAN.md)。
