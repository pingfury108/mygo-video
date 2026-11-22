package plugin

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const demoManifest = `{
  "id": "demo",
  "name": "Demo",
  "version": "0.1.0",
  "apiVersion": 1,
  "main": "index.js",
  "permissions": {"network": ["127.0.0.1"]},
  "settings": [{"key": "region", "label": "Region"}]
}`

const demoJS = `
export async function home() {
  return { title: "Demo", sections: [
    { title: "Trending", videos: [
      { id: "v1", name: "First Video", duration: 120000 },
      { id: "v2", name: "Second Video", series: "Show", season: 1, episode: 2 },
    ]},
  ]};
}

export async function search({ query, offset }) {
  host.storage.set("last", query);
  return { videos: [{ id: "s1", name: "Found " + query + " at " + offset }], more: true };
}

export function lastQuery() { return host.storage.get("last") || ""; }

export async function title({ id }) {
  return {
    video: { id, name: "Show", series: "Show" },
    episodes: [{ id: "e1", series: "Show", season: 1, episode: 1 }],
  };
}

export async function play({ id }) {
  if (id === "drm") return { drm: "widevine" };
  if (id === "open") return { open: "https://example.com/watch" };
  if (id === "fail") throw Object.assign(new Error("nope"), { code: "signIn" });
  return {
    url: "https://cdn.example.com/" + id + ".m3u8",
    type: "hls",
    headers: { referer: "https://example.com/" },
    duration: 5000,
  };
}

export function signIn() { return { id: "u1", name: "Alex" }; }
export function signOut() {}
export function account() { return { id: "u1", name: "Alex" }; }
export async function progress() {}
`

// loadDemo writes the demo plugin and loads it.
func loadDemo(t *testing.T) *Plugin {
	t.Helper()
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(demoManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte(demoJS), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(pluginDir, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAndInfo(t *testing.T) {
	p := loadDemo(t)
	defer p.Close()

	info := p.Info()
	if info.ID != "demo" || info.Name != "Demo" || info.Playback != "stream" {
		t.Fatalf("info %+v", info)
	}
	if len(info.Settings) != 1 || info.Settings[0].Key != "region" {
		t.Fatalf("settings %+v", info.Settings)
	}
	if !p.Can("search") || !p.Can("play") || p.Can("like") {
		t.Fatalf("can: search %v play %v like %v", p.Can("search"), p.Can("play"), p.Can("like"))
	}
}

func TestHome(t *testing.T) {
	p := loadDemo(t)
	defer p.Close()
	b, err := p.Home()
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Demo" || len(b.Sections) != 1 || len(b.Sections[0].Videos) != 2 {
		t.Fatalf("home %+v", b)
	}
	if b.Sections[0].Videos[1].Series != "Show" || b.Sections[0].Videos[1].Episode != 2 {
		t.Fatalf("second video %+v", b.Sections[0].Videos[1])
	}
}

func TestSearchAndStorage(t *testing.T) {
	p := loadDemo(t)
	defer p.Close()
	r, err := p.Search("fog", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Videos) != 1 || r.Videos[0].Name != "Found fog at 10" || !r.More {
		t.Fatalf("search %+v", r)
	}
	var last string
	if err := p.call("lastQuery", nil, &last); err != nil {
		t.Fatal(err)
	}
	if last != "fog" {
		t.Fatalf("stored %q", last)
	}
}

func TestPlay(t *testing.T) {
	p := loadDemo(t)
	defer p.Close()
	pb, err := p.Play("v1", "1080")
	if err != nil {
		t.Fatal(err)
	}
	if pb.URL != "https://cdn.example.com/v1.m3u8" || pb.Type != "hls" {
		t.Fatalf("play %+v", pb)
	}
	if pb.Headers["referer"] != "https://example.com/" {
		t.Fatalf("headers %+v", pb.Headers)
	}
	if pb.Duration != 5000 {
		t.Fatalf("duration %d", pb.Duration)
	}

	if _, err := p.Play("drm", ""); !errors.Is(err, ErrDRM) {
		t.Fatalf("drm err %v", err)
	}
	if _, err := p.Play("open", ""); err != nil {
		t.Fatal(err)
	}
}

func TestErrorCode(t *testing.T) {
	p := loadDemo(t)
	defer p.Close()
	if _, err := p.Play("fail", ""); !errors.Is(err, ErrSignIn) {
		t.Fatalf("err %v, want sign in", err)
	}
}

func TestTitleAndAccount(t *testing.T) {
	p := loadDemo(t)
	defer p.Close()
	tp, err := p.Title("show")
	if err != nil {
		t.Fatal(err)
	}
	if tp.Video.ID != "show" || len(tp.Episodes) != 1 {
		t.Fatalf("title %+v", tp)
	}
	a, err := p.SignIn()
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "u1" || a.Name != "Alex" {
		t.Fatalf("account %+v", a)
	}
}

// The plugin fetches only the hosts its manifest names, and reaches one.
func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/data" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, "fetched")
	}))
	defer srv.Close()

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "fetch")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(demoManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	js := `
export async function search() {
  const res = await fetch("` + srv.URL + `/data");
  return { videos: [{ id: "x", name: await res.text() }] };
}
`
	if err := os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(pluginDir, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r, err := p.Search("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Videos) != 1 || r.Videos[0].Name != "fetched" {
		t.Fatalf("search %+v", r)
	}
}

// A plugin asking for a host it didn't name reaches nothing.
func TestFetchDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "secret")
	}))
	defer srv.Close()

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "denied")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := strings.Replace(demoManifest, `["127.0.0.1"]`, `["api.example.com"]`, 1)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	js := `
export async function search() {
  try {
    await fetch("` + srv.URL + `/data");
    return { videos: [{ id: "x", name: "reached" }] };
  } catch (e) {
    return { videos: [{ id: "x", name: "denied" }] };
  }
}
`
	if err := os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(pluginDir, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r, err := p.Search("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.Videos[0].Name != "denied" {
		t.Fatalf("got %q, want denied", r.Videos[0].Name)
	}
}

func TestManifestIDMustBeSafe(t *testing.T) {
	dir := t.TempDir()
	manifest := strings.Replace(demoManifest, `"demo"`, `"../outside"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest(dir); err == nil {
		t.Fatal("readManifest accepted an id that leaves the plugin folder")
	}
}

func TestMinAppVersion(t *testing.T) {
	old := appVersion
	appVersion = "0.9.0"
	defer func() { appVersion = old }()

	dir := t.TempDir()
	manifest := strings.Replace(demoManifest, `"apiVersion": 1,`, `"apiVersion": 1, "minAppVersion": "0.10.0",`, 1)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest(dir); err == nil {
		t.Fatal("readManifest accepted a plugin that needs a newer app")
	}

	appVersion = "0.10.0 dev"
	if _, err := readManifest(dir); err != nil {
		t.Fatalf("readManifest rejected a plugin for this app: %v", err)
	}
}

func TestStreamHostsFallBackToNetwork(t *testing.T) {
	p := loadDemo(t)
	defer p.Close()
	if !p.AllowsStreamHost("http://127.0.0.1/video.mp4") {
		t.Fatal("a plugin without streamHosts may not play a host it may fetch")
	}
	if p.AllowsStreamHost("https://evil.example/video.mp4") {
		t.Fatal("a host outside the network permission was allowed")
	}
}
