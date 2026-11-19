# Changelog

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
