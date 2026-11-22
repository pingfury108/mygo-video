# Changelog

## 0.1.24

- Windows 的窗口按钮改为覆盖在所有页面之上：视频播放页、选集抽屉和播放中的提示
  都会避开右上角的最小化、最大化和关闭按钮；全屏时不再叠出第二套控件。
- macOS 播放：AVFoundation 不认的 MKV、AVI、WebM 等容器在打开前交给 ffmpeg 转
  封装；已经播过又中断的视频可以从当前进度重新连接；HEVC 探测带上请求头和超时，
  并缓存结果。
- HLS 与 WebDAV 播放的时长、进度按 ffmpeg 读到的真实文件长度计算，事件播放列表
  写到哪里不再被误判为播完；seek 落定前保存进度不会记成旧位置。
- 局域网 WebDAV、IPTV 地址不再走系统代理：ffmpeg 探测、转封装和缩略图都带
  `-http_proxy ""`，代理只影响公网请求；修复直播重试状态未声明导致的构建失败。
- 插件系统加固：清单 ID 做路径安全校验，`minAppVersion` 生效，登录页只允许
  http/https；`play.open`、`host.openURL`、WebSocket 都按插件 host 权限校验，
  未声明 `streamHosts` 时回退到 `network`，旧插件继续可用；WebSocket 握手期间
  发送会排队，关闭只通知一次。
- 插件页面与搜索按代次丢弃过期结果，加载失败不再每帧重试；切换来源会清掉进行中
  的状态；禁用、卸载和重新加载会清理账号缓存；zip 和文件夹安装统一限制 100MB、
  2000 个文件，并按实际写入字节校验，复制失败会清理半成品目录；插件存储限制为
  4MB、1000 个 key。

## 0.1.23

- WebDAV 的 GPU 解码补上 Authorization 头，服务器需要认证时不再打不开；交给
  mpv 播放时也带上同样的头。
- 补全 GPU 播放的打开、seek 和关闭流程：关闭时打断还在等网络的读取，等待正在
  进行的 seek 返回后再释放解码器；打开失败会释放已经建好的 D3D11 设备，反复
  重试不再泄漏。
- GPU 解码失败后按 GPU → mpv → remux → 转码的顺序回退；mpv 不在时，remux
  会从当前播放位置开始，进度和字幕仍按整部影片计时，拖动进度条也不会反复重试
  已经失败的 GPU 路径。
- 音频按设备真实的混音格式输出：识别 `WAVE_FORMAT_EXTENSIBLE` 的子格式和有效
  位数，支持 float、16/24/32-bit PCM（含 24-bit 打包），不再把 PCM 当 float
  写进声卡；音量、静音和倍速真正接到 WASAPI 会话上。
- 播完可以重新播放，暂停时拖动进度条也会显示新位置；声音设备或解码出错会转成
  播放失败，由上层自动换一种方式打开，而不是留一个静音画面。
- 字幕改用整部影片的播放位置，WebDAV remux 从中间起播时不再整体偏移。
- 颜色按每帧的 limited/full range 和 BT.601/709 交给 D3D11 视频处理器，减少
  发灰和偏色。
- HEVC 探测结果会缓存并带超时，插件请求返回时校验代次，日志超过 4MB 后重建，
  避免重复卡顿和日志无限增长。
- 实测同一 4K HEVC WebDAV 视频：连续播放 30 秒与从 300 秒 seek 后，稳态音画
  误差都在 ±40ms 内，没有累积漂移。

## 0.1.22

- 修复 4K HEVC 播放音画不同步：GPU 解码此前把所有画面转换到同一张纹理，解码
  线程提前约一秒时，屏幕显示的其实是最新解码帧，而不是引擎选中的那一帧；现为
  每个排队画面分配独立纹理槽位，并在显示、丢弃或 seek 后回收。实测起播与连续
  seek 后画面与声音稳定在一帧以内，不再累积漂移。
- 视频和音频的时间戳按 mpv 的方式读取 `best_effort_timestamp`，并设置
  `pkt_timebase`；视频改回影片自身的时间轴，seek 后不再重复叠加位置。
- 播放器新增 `[` / `]` 手动微调音画偏移，每次 20ms 并按机器保存，用于个别
  显示设备或声卡本身的延迟差异。

## 0.1.21

- 修复播放放大后缩不回来：离开播放器时窗口没有退出全屏，而无边框全屏的窗口
  既不能拖动也拖不出大小，后续页面再点最大化就把状态彻底弄乱。现在离开播放器
  即退出全屏，最大化按钮识别全屏状态，双击标题栏在全屏时退出全屏。
- 网格铺满整页：插件首页、详情页剧组、片库、搜索、WebDAV 全部视频原来都是
  固定列数加固定卡片宽，宽窗口右侧留一大片空白；现统一按窗口宽度算列数，
  每张卡等分整行。
- 页头与侧栏对齐：页面标题的垂直位置与左侧应用标识同一线，右侧内容底部也留出
  与侧栏相同的边距。

## 0.1.20

- 音画同步按 mpv 的做法量化与修正：画面按每帧自身 PTS 落在音频时钟上，落后
  超过半秒时解码器跳过非参考帧追赶，落后超过两秒半时直接瞬移。实测 4K HEVC
  连播 45 分钟、从中间起播、反复 seek：画面始终在声音一帧（0~30ms）以内，
  无累积漂移。
- 修复 seek 后画面冻结：seek 期间解码中的帧曾被记到新位置，堵在队头挡住画面
  达十余秒；现按世代丢弃，且不污染新位置的 PTS 基准。
- 修复网络抖动导致永久停摆：读包失败曾被当作文件结束，音视频两侧均改为重试
  （等待期间松开解码锁，seek 不被阻塞）。
- 数据源读前缓冲加深至 512KB 并自动重连；音频结束后画面按墙钟继续播到片尾，
  不再冻结在最后一声音。
- 播放器：进度条悬停显示所指时间、点击直达、跳转等待期回旋钮不再回弹；
  自动连播与「已看完」标记修正。
- LibreTV 插件：豆瓣热门首页、聚合搜索、m3u8 直连播放，源清单从部署站点运行
  时拉取。

## 0.1.19

- LibreTV 插件：聚合搜索采集站、豆瓣热门首页、m3u8 直连播放；源清单从部署的
  tv 站运行时拉取，换源无需更新插件；详情页保留豆瓣封面、描述清洗为纯文本；
  播放地址与网页版同路径直连源站。
- 自动连播修复：播放器只在首帧时上报一次 Ended，之后永远不报——改用播放头
  距结尾半秒判定；音频结束时保留最后位置（此前播放头会跳回零）；seek 落点
  与看完判定线的三个常数排齐，看过的集正确标记。
- 播放器进度条：悬停显示所指时间，点击直达；拖拽只在松手时提交一次；跳转
  等待期旋钮钉在目标位置不再回弹（此前 WebDAV 转码重启期间来回跳）。
- 视频画面按每帧自身的 PTS 对齐音频时钟（此前按帧数×平均帧率推算，VFR 与
  流起点偏移会累积漂移）；解码器 seek 与解码不再并发竞争；视频队列加深到约
  一秒，网络抖动时画面不再先于声音卡住。
- 插件权限拆分：数据抓取与播放地址分开授权。
- CI：Windows 构建携带 cgo 与解码所需的 ffmpeg DLL。

## 0.1.18

- Windows plays a 4K HEVC video on the GPU: libavcodec decodes it with
  Direct3D 11 and the renderer draws the texture where the decoder left it,
  through a shared one a video processor turned from NV12 to BGRA on the GPU.
  The sound plays through WASAPI, and its device position is the clock the
  picture follows. A transcode, which kept the CPU busy beside a picture it
  could not carry at 4K, is gone from this path.
- macOS plays it as it is too: an fMP4 remux writes the hvcC box that the
  hev1 tag leaves out, which the frame output AVFoundation gives MyGo reads.
  The stream is copied, not encoded, so the picture is the file's own.
- The player's controls: a click on the picture pauses it and the bars over it
  keep their own presses, the progress bar sits between the steps and the
  sound, every control says what it does when the pointer rests on it, the
  rate opens a list from 0.5× to 2×, and a drag of the progress bar seeks once
  when it ends, rather than restarting the remux at every step.
- Episodes fill the width of their row, and the title bar drags the window
  again, as it did before the bars over the picture grew.

## 0.1.17

- Windows plays a video in mpv, drawn into the app's own window: mpv decodes
  HEVC in hardware and draws the picture with the GPU, so a 4K film plays as
  it is, where Media Foundation had none of it and a transcode kept the CPU
  busy. The web view steps aside while mpv runs and comes back when it ends,
  with the position kept.
- The mpv of the Windows build comes with the app, and ffmpeg stays for the
  fallback the other platforms use.

## 0.1.16

- Transcodes encode with x264 and scale the picture to 1080p at most: on a
  Pascal card (GTX 1050 Ti) with driver 582.78, NVENC took the first frame
  and crashed (0xc0000005) with ffmpeg 7.1 and exited silently with 4.4.1,
  while x264 4K-to-1080p kept a 0.93x pace, close enough to playback. The GPU
  encoder probe is gone with it.
- On Windows a transcode decodes with d3d11va, the one that takes HEVC.

## 0.1.15

- The GPU encoder check writes a file instead of stdout: a windowed app has
  no console, and NVENC crashed (0xc0000005) on the pipe it got instead,
  while the same command ran fine from a terminal.

## 0.1.14

- Windows carries ffmpeg 7.1 rather than a master build: the master builds
  want NVENC API 13.1 and a 610 driver, so a machine on an older driver fell
  back to x264. 7.1 asks for 12.1, which drivers from 550 take.

## 0.1.13

- The GPU encoder check asks each one to encode a second of a test picture,
  yuv420p or nv12 as it takes, within a timeout, and no longer settles for
  Media Foundation's software encoder: the old check handed NVENC rgb24 and
  gave up, so a machine with a GPU still transcoded on the CPU.
- A version tag publishes a release; pushes to main no longer run the build.

## 0.1.12

- The check for a GPU encoder failed on machines that have one: it handed
  NVENC a picture in rgb24, which it turns down. A yuv420p test picture
  finds NVENC, Quick Sync and AMF, so a 4K transcode runs on the GPU.

## 0.1.11

- A transcode keeps the picture's size and encodes with the GPU's encoder
  (NVENC, Quick Sync or AMF) where one works, which keeps a 4K picture ahead
  of playback; x264 is the fallback.

## 0.1.10

- A transcoded stream decodes on the GPU where there is one, is scaled to
  1080p at most and uses 2-second parts: a 4K H.264 encode at full size does
  not keep up with playback, which made it stall every few seconds.
- When Microsoft's HEVC decoder is installed, HEVC plays as it is instead of
  being transcoded.

## 0.1.9

- The three ffmpeg probes at launch no longer flash console windows on
  Windows.
- A stream whose video is HEVC is transcoded before it plays on Windows:
  Media Foundation would play its audio and leave the picture black, with no
  error to notice, so the app asks ffmpeg first.

## 0.1.8

- HLS parts are served as `video/mp2t`: Media Foundation turns down a TS
  part sent as `video/mp4`, and waits forever instead of saying so, which
  is why a remuxed or transcoded file never started on Windows.
- The playlist is waited for 8 seconds rather than 20, so a player that
  cannot start says so and the app tries the next way in.
- HLS parts go in the data directory's `tmp/` instead of the system's
  temporary directory, so a data directory on another drive holds them too.
  Leftovers of a run that ended are removed at launch.
- Only the version before gets a delta update, which keeps the release page
  short while the app is young.

## 0.1.7

- HLS segments are MPEG-TS: Media Foundation plays those, not the fMP4
  ones the proxy wrote before, so a remuxed or transcoded file still failed
  on Windows even after ffmpeg took over.
- `diag.log` now says what happened: the version, what ffmpeg is and which
  encoders it has, every stream opened, every retry, what ffmpeg prints,
  and the player's error.

## 0.1.6

- A file the system's player has no decoder for, such as HEVC on Windows
  without Microsoft's extension, is transcoded to H.264 instead of failing:
  the GPU decodes it where there is one, x264 encodes it.
- The bundled ffmpeg is the GPL build, which carries x264 for that.

## 0.1.5

- A stream the system's player refuses is retried through ffmpeg as soon
  as the player reports the error, not only when opening the URL fails:
  Media Foundation reports its refusals after the URL has opened.

## 0.1.4

- WebDAV, IPTV and plugin streams get the same remux fallback as local
  files: when Media Foundation turns a stream down, ffmpeg remuxes it into
  HLS instead of showing the error.

## 0.1.3

- A "Check for Updates…" item in the app menu, and on Windows and Linux in
  a menu bar, which shows the update window at once.

## 0.1.2

- Windows: the app draws its own minimize, maximize and close buttons, so
  they show on the dark title bar instead of blending into it.
- Windows: a file Media Foundation turns down is remuxed to HLS with ffmpeg
  and played from there, which covers most MKV files.
- Updates: the app checks GitHub releases for a newer version, shows its
  notes, downloads it with a progress bar and relaunches into it.

## 0.1.1

- Windows: ffmpeg runs without a console window, so thumbnails and remuxes
  no longer flash a black window.
- Windows: a second launch brings the window of the first one to the front
  instead of quietly handing over and exiting.
- What stops the app before it runs goes to `startup-error.log`.

## 0.1.0

- First release: local library, WebDAV, IPTV, and plugins for Bilibili,
  Youku and Tencent Video.
