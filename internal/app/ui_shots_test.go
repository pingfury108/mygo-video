package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mygo-video/internal/local"
	"mygo-video/internal/media"
	"mygo-video/internal/plugin"
	"mygo-video/internal/store"
	"mygo-video/internal/webdav"
)

// TestShots renders the pages without a window, as fast as a unit test, and
// writes them where SHOTS points, for looking over the layout without a
// pointer, a screen recording or a window at a known place.
//
//	SHOTS=/tmp/mygo-video-shots go test ./internal/app -run TestShots
func TestShots(t *testing.T) {
	dir := os.Getenv("SHOTS")
	if dir == "" {
		t.Skip("SHOTS is not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := shotApp(t)
	tt := ui.NewTester(a.view, 1180, 780)

	shoot := func(name string) {
		t.Helper()
		tt.Frame()
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}

	a.route = pageHome
	shoot("01-home.png")

	a.sourceMenu = true
	shoot("08-source-menu.png")
	a.sourceMenu = false

	a.route = pageDetail
	a.current = a.lib.Items()[0]
	shoot("02-detail.png")

	a.route = pageLibrary
	shoot("03-library.png")

	a.route = pageSearch
	a.query = ""
	shoot("04-search.png")

	a.query = "雾"
	shoot("05-search-results.png")

	a.route = pageSettings
	shoot("06-settings.png")

	// The sidebar with nothing watched yet, and a film on its page.
	a.route = pageHome
	a.store = freshStore(t)
	shoot("07-home-empty.png")
}

// shotApp is an app of made-up videos, one watched to a third.
func shotApp(t *testing.T) *app {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{
		store:     st,
		dataDir:   dir,
		lib:       local.New(nil),
		thumbs:    map[string]*ui.Bitmap{},
		thumbBusy: map[string]bool{},
		thumbUsed: map[string]time.Time{},
		volume:    0.8,
	}
	items := []media.Item{
		{ID: "a1", Name: "雾港", Path: "/videos/雾港.S02E05.mp4", Ext: ".mp4", Size: 1_402_000_000,
			ModTime: time.Now().Add(-2 * time.Hour), Series: "雾港", Season: 2, Episode: 5},
		{ID: "a2", Name: "雾港", Path: "/videos/雾港.S02E06.mp4", Ext: ".mp4", Size: 1_380_000_000,
			ModTime: time.Now().Add(-3 * time.Hour), Series: "雾港", Season: 2, Episode: 6},
		{ID: "b1", Name: "长夜列车", Path: "/videos/长夜列车.S01E01.mp4", Ext: ".mp4", Size: 2_100_000_000,
			ModTime: time.Now().Add(-26 * time.Hour), Series: "长夜列车", Season: 1, Episode: 1},
		{ID: "c1", Name: "山雨", Path: "/videos/山雨.mp4", Ext: ".mp4", Size: 3_200_000_000,
			ModTime: time.Now().Add(-48 * time.Hour)},
		{ID: "d1", Name: "婚礼请柬-手帐拼贴风", Path: "/videos/wedding.mp4", Ext: ".mp4", Size: 1_300_000,
			ModTime: time.Now().Add(-72 * time.Hour)},
	}
	a.lib.SetCached(items, time.Now())
	a.current = items[0]
	a.thumbs[items[0].ID] = fakeFrame(640, 360, color.RGBA{58, 92, 120, 255})
	a.thumbs[items[2].ID] = fakeFrame(640, 360, color.RGBA{96, 74, 52, 255})
	a.thumbs[items[3].ID] = fakeFrame(640, 360, color.RGBA{52, 78, 58, 255})
	st.SetProgress(items[0].ID, media.Progress{
		Position: 7 * 60 * 1000,
		Duration: 43 * 60 * 1000,
		Updated:  time.Now().Add(-20 * time.Minute).UnixMilli(),
	})
	return a
}

func freshStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// fakeFrame is a picture standing in for a frame of a video.
func fakeFrame(w, h int, base color.RGBA) *ui.Bitmap {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(min(255, int(base.R)+x*90/w)),
				G: uint8(min(255, int(base.G)+y*70/h)),
				B: uint8(min(255, int(base.B)+40)),
				A: 255,
			})
		}
	}
	return ui.NewBitmap(img)
}

// TestPluginShots renders a plugin's home and a video of it, with the
// videos filled in as the plugin would have listed them.
func TestPluginShots(t *testing.T) {
	dir := os.Getenv("SHOTS")
	if dir == "" {
		t.Skip("SHOTS is not set")
	}
	a := shotApp(t)
	a.plugins = []*plugin.Plugin{loadTestPlugin(t)}
	a.source = 2
	v := media.Video{
		ID:       "v1",
		Name:     "BipBop",
		Overview: "Apple 的公开 HLS 测试流，用来验证代理、HLS 重写和播放。",
	}
	a.pluginBrowse = &media.Browse{
		Title:    "演示源",
		Subtitle: "公开的 HLS 测试流",
		Sections: []media.Section{{Title: "测试流", Videos: []media.Video{v}}},
	}
	a.pluginItems = []media.Item{itemFromVideo("demo", v)}
	a.current = a.pluginItems[0]
	a.settingTexts = map[string]*string{}
	a.pluginAccounts = map[string]*plugin.Account{}
	a.pluginAccountBusy = map[string]bool{}
	a.pluginChecked = map[string]bool{"demo": true}
	a.pluginsDir = "/tmp/mygo-video-plugins"
	a.posters = map[string]*ui.Bitmap{}
	a.posterBusy = map[string]bool{}
	a.posterUsed = map[string]time.Time{}

	tt := ui.NewTester(a.view, 1180, 780)
	shoot := func(name string) {
		t.Helper()
		tt.Frame()
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	a.route = pageHome
	shoot("09-plugin-home.png")
	a.route = pageDetail
	shoot("10-plugin-detail.png")
	a.route = pageSettings
	shoot("11-plugin-settings.png")
	a.installMenu = true
	shoot("12-install-menu.png")
	a.installMenu = false
	a.installDialog = true
	a.installURL = "egoist/mygo-music-plugin"
	shoot("13-install-github.png")
	a.installDialog = false
}

func loadTestPlugin(t *testing.T) *plugin.Plugin {
	t.Helper()
	dir := t.TempDir()
	pd := filepath.Join(dir, "plugins", "demo")
	if err := os.MkdirAll(pd, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"demo","name":"演示源","version":"0.1.0","apiVersion":1,"main":"index.js","permissions":{"network":["example.com"]},"settings":[{"key":"region","label":"地区","help":"如 cn"}]}`
	js := `export function home(){return {title:"Demo",sections:[]}};
export function play(){return {url:"https://example.com/x.m3u8",type:"hls"}}`
	if err := os.WriteFile(filepath.Join(pd, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pd, "index.js"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := plugin.Load(pd, filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestWebDAVShots renders the WebDAV source with a folder and two videos,
// as a server would have listed them.
func TestWebDAVShots(t *testing.T) {
	dir := os.Getenv("SHOTS")
	if dir == "" {
		t.Skip("SHOTS is not set")
	}
	a := shotApp(t)
	c, err := webdav.New(webdav.Config{URL: "http://127.0.0.1:1/dav"})
	if err != nil {
		t.Fatal(err)
	}
	a.webdav = c
	a.webdavURL = "http://127.0.0.1:1/dav"
	a.source = sourceWebDAV
	a.webdavLoaded = ""
	a.webdavHasLoaded = true
	a.webdavEntries = []webdav.Entry{
		{Path: "Movies", Name: "Movies", Dir: true},
		{Path: "Shows", Name: "Shows", Dir: true},
	}
	a.webdavVideos = a.webdavItemsFrom([]webdav.Entry{
		{Path: "Movies/Heat (1995).mkv", Name: "Heat (1995).mkv", Size: 8_000_000_000},
		{Path: "Movies/Dune.mkv", Name: "Dune.mkv", Size: 6_000_000_000},
	})
	tt := ui.NewTester(a.view, 1180, 780)
	shoot := func(name string) {
		t.Helper()
		tt.Frame()
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	a.route = pageHome
	shoot("14-webdav-home.png")
	a.current = a.webdavVideos[0]
	a.route = pageDetail
	shoot("15-webdav-detail.png")
}

// TestWebDAVAllShot renders the grid of every video found below.
func TestWebDAVAllShot(t *testing.T) {
	dir := os.Getenv("SHOTS")
	if dir == "" {
		t.Skip("SHOTS is not set")
	}
	a := shotApp(t)
	c, err := webdav.New(webdav.Config{URL: "http://127.0.0.1:1/dav"})
	if err != nil {
		t.Fatal(err)
	}
	a.webdav = c
	a.source = sourceWebDAV
	a.webdavHasLoaded = true
	a.webdavAll = true
	a.webdavAllItems = a.webdavItemsFrom([]webdav.Entry{
		{Path: "Movies/Heat (1995).mkv", Name: "Heat (1995).mkv", Size: 8_000_000_000},
		{Path: "Movies/Dune.mkv", Name: "Dune.mkv", Size: 6_000_000_000},
		{Path: "Shows/Fog Harbour S02E05.mp4", Name: "Fog Harbour S02E05.mp4", Size: 1_200_000_000},
	})
	a.route = pageHome
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	tt.Frame()
	f, err := os.Create(filepath.Join(dir, "16-webdav-all.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
