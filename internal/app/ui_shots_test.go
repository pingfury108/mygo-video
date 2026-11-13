package app

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mygo-video/internal/iptv"
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
	p := loadTestPlugin(t)
	a.plugins = []*pluginEntry{{dir: "/tmp/demo", man: plugin.Manifest{Info: p.Info()}, p: p}}
	a.source = sourcePluginBase
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
	a.webdavDirs = map[string]webdavDir{
		"": {At: time.Now().UnixMilli(), Entries: a.webdavEntries},
	}
	a.webdavIndexAt = time.Now()
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

// TestIPTVShot renders the live source with a few channels in two groups.
func TestIPTVShot(t *testing.T) {
	dir := os.Getenv("SHOTS")
	if dir == "" {
		t.Skip("SHOTS is not set")
	}
	a := shotApp(t)
	a.source = sourceIPTV
	a.iptvURL = "https://example.com/live.m3u"
	a.iptvLoaded = true
	a.iptvChannels = []iptv.Channel{
		{Name: "CCTV-1 综合", Group: "央视", URL: "http://example.com/1.m3u8"},
		{Name: "CCTV-5 体育", Group: "央视", URL: "http://example.com/5.m3u8"},
		{Name: "CCTV-6 电影", Group: "央视", URL: "http://example.com/6.m3u8"},
		{Name: "湖南卫视", Group: "卫视", URL: "http://example.com/hunan.m3u8"},
		{Name: "东方卫视", Group: "卫视", URL: "http://example.com/dongfang.m3u8"},
		{Name: "浙江卫视", Group: "卫视", URL: "http://example.com/zhejiang.m3u8"},
	}
	a.iptvGroups = []string{"央视", "卫视"}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	tt.Frame()
	f, err := os.Create(filepath.Join(dir, "17-iptv-home.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

// TestPluginsShot renders the plugins page with a plugin open, and the
// list with the ones switched off.
func TestPluginsShot(t *testing.T) {
	dir := os.Getenv("SHOTS")
	if dir == "" {
		t.Skip("SHOTS is not set")
	}
	a := shotApp(t)
	p := loadTestPlugin(t)
	a.plugins = []*pluginEntry{
		{
			dir: "/tmp/plugins/tencent",
			man: plugin.Manifest{
				Info:        media.Info{ID: "tencent", Name: "腾讯视频", Version: "1.2.0"},
				APIVersion:  1,
				Description: "目录、搜索与追剧；受保护的正片在官方播放页打开。",
				Permissions: plugin.Permissions{Network: []string{"v.qq.com"}},
				Login:       &plugin.Login{URL: "https://passport.qq.com/login", CookieHosts: []string{"v.qq.com"}},
			},
			state: store.PluginState{Enabled: true, EnabledAt: 1, InstalledAt: 1},
			p:     p,
		},
		{
			dir:   "/tmp/plugins/demo",
			man:   plugin.Manifest{Info: p.Info(), APIVersion: 1, Description: "公开的 HLS 测试流。"},
			p:     p,
			state: store.PluginState{Enabled: true, EnabledAt: 1, InstalledAt: 1},
		},
		{
			dir: "/tmp/plugins/youku",
			man: plugin.Manifest{
				Info:        media.Info{ID: "youku", Name: "优酷", Version: "1.0.4"},
				APIVersion:  1,
				Description: "公开目录与搜索；登录后可同步追剧。",
				Permissions: plugin.Permissions{Network: []string{"youku.com"}},
			},
		},
		{
			dir:   "/tmp/plugins/jellyfin",
			man:   plugin.Manifest{Info: media.Info{ID: "jellyfin", Name: "Jellyfin", Version: "0.3.0"}, APIVersion: 1, Description: "连接自己的服务器，播放其中的影片。"},
			state: store.PluginState{InstalledAt: 1},
		},
		{
			dir: "/tmp/plugins/old",
			man: plugin.Manifest{Info: media.Info{ID: "old", Name: "自建源", Version: "0.1.2"}, APIVersion: 1},
			err: errors.New("需要 apiVersion 2，当前应用只支持 1"),
		},
	}
	a.source = sourceLocal
	a.pluginOpen = "builtin:webdav"
	a.iptvURL = "https://iptv-org.github.io/iptv/countries/cn.m3u"
	a.iptvLoaded = true
	a.iptvChannels = []iptv.Channel{{Name: "CCTV-1", Group: "央视"}}
	a.iptvGroups = []string{"央视"}
	a.settingTexts = map[string]*string{}
	a.pluginAccounts = map[string]*plugin.Account{}
	a.pluginAccountBusy = map[string]bool{}
	a.pluginChecked = map[string]bool{}
	a.route = pagePlugins

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
	shoot("18-plugins-builtin.png")
	a.pluginOpen = "tencent"
	shoot("19-plugins-open.png")
	a.pluginOpen = ""
	a.pluginFilter = "off"
	shoot("20-plugins-off.png")
}
