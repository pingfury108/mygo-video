# Third-party notices

The app's own code is MIT (see [LICENSE](LICENSE)). It uses and, in the
builds CI publishes, bundles the following:

## ffmpeg

Thumbnails, WebDAV remuxing, DASH joining and duration probing use ffmpeg.
The release builds bundle a static ffmpeg:

| Platform | Source | License |
| --- | --- | --- |
| macOS arm64 / amd64 | [eugeneware/ffmpeg-static](https://github.com/eugeneware/ffmpeg-static) (static builds from evermeet.cx) | GPL |
| Windows x64 / Linux x64 | [BtbN/FFmpeg-Builds](https://github.com/BtbN/FFmpeg-Builds) (LGPL builds) | LGPL |

ffmpeg is a trademark of Fabrice Bellard, originator of the FFmpeg project.
See <https://ffmpeg.org/legal.html> for the licenses and what they require.
The bundled program is unmodified; its source is available from the
projects above and from <https://ffmpeg.org/download.html>.

If you build the app yourself, ffmpeg is not bundled: the app looks for one
in the settings, in its own `resources/bin`, or on the `PATH`.

## Go modules

See `go.mod` and `go.sum`. The notable ones:

- [MyGo](https://github.com/egoist/mygo) — MIT
- [moejs](https://github.com/MoeForge/moejs) — the JavaScript runtime the
  plugins run in
- [coder/websocket](https://github.com/coder/websocket) — ISC

## Fonts and icons

The app draws with the system's fonts. The app icon and the interface are
the project's own.
