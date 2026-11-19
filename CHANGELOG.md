# Changelog

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
