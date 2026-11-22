// The app's configuration, evaluated by `mygo dev` and `mygo build` with Bun.
export default ({ command }: { command: string }) => ({
  name: "MyGo Video",
  identifier: "dev.pingfury.video",
  slug: "mygo-video",
  version: "0.1.24",
  out: "build",
  updates: {
    publicKey: "CQ2rZMu1lVUSqWc89oe9v+e7ddSZGdUdDDMgiw98p7A=",
    github: "pingfury108/mygo-video",
    // While the app is young, only the version before gets a delta.
    deltas: 1,
  },
  urlSchemes: [command === "dev" ? "mygo-video-dev" : "mygo-video"],
  macos: {
    minimumSystemVersion: "13.0",
  },
  linux: {
    comment: "A native video player, with plugins for more sources",
    categories: ["AudioVideo", "Video", "Player"],
    webview: false,
  },
});
