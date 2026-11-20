# Changelog

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
