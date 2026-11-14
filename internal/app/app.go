package app

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"

	"mygo-video/internal/ffbin"
	"mygo-video/internal/iptv"
	"mygo-video/internal/local"
	"mygo-video/internal/media"
	"mygo-video/internal/plugin"
	"mygo-video/internal/store"
	"mygo-video/internal/streamproxy"
	"mygo-video/internal/subtitle"
	"mygo-video/internal/webdav"
)

const appName = "MyGo Video"

// The sources that come before the plugins: the library on this computer,
// the WebDAV server it reads, and the live playlist.
const (
	sourceLocal      = 0
	sourceWebDAV     = 1
	sourceIPTV       = 2
	sourcePluginBase = 3
)

// processStart is when the app's code began, before main: the mark of a dev
// run that says where startup spends its time.
var processStart = time.Now()

type page int

const (
	pageHome page = iota
	pageLibrary
	pageSearch
	pagePlugins
	pageSettings
	pageDetail
	pagePlayer
	pageSignIn
)

// app is the state every view reads and changes, on the main thread.
type app struct {
	win   *mygo.Window
	lib   *local.Library
	store *store.Store

	// thumbs are the frames taken of the videos showing, by item, and the
	// ones loading or loaded. Main thread only.
	thumbDir  string
	thumbs    map[string]*ui.Bitmap
	thumbBusy map[string]bool
	thumbUsed map[string]time.Time

	// ffmpeg is the program the app uses, wherever it is: the listener's
	// choice, the app's own resources, or the PATH.
	ffmpeg  string
	toolBin string
	// thumbSem keeps ffmpeg jobs to a couple at a time, for a server that
	// would feel dozens of them.
	thumbSem chan struct{}

	// posters are the pictures plugins give for their videos, by URL.
	posters    map[string]*ui.Bitmap
	posterBusy map[string]bool
	posterUsed map[string]time.Time

	route   page
	current media.Item
	query   string
	dataDir string
	// sourceMenu shows the list of sources, from the sidebar's foot.
	sourceMenu bool

	// plugins are the sources dropped in, none started yet; proxy is where
	// their streams are served to the player. source is 0 for the library
	// on this computer, and else the plugin at source-1.
	plugins    []*pluginEntry
	proxy      *streamproxy.Proxy
	source     int
	proxyToken string

	// The plugin pages: which settings are being typed, who is signed in,
	// and where plugins are installed from.
	pluginsDir        string
	pluginDataDir     string
	settingTexts      map[string]*string
	pluginAccounts    map[string]*plugin.Account
	pluginAccountBusy map[string]bool
	pluginChecked     map[string]bool

	// installMenu and installDialog are the ways to install a plugin, and
	// where a GitHub address is typed.
	installMenu   bool
	installDialog bool
	installFocus  bool
	installURL    string
	installBusy   bool
	installErr    string

	// What the source being shown has: its home page, the videos of it,
	// the search last run, and the error that stopped them.
	pluginBrowse *media.Browse
	pluginItems  []media.Item
	pluginSearch []media.Item
	pluginErr    string
	pluginBusy   bool
	pluginTab    string
	searchQuery  string
	searchBusy   bool

	// The plugins page: which plugin is open, and how the list is filtered.
	pluginOpen   string
	pluginFilter string
	pluginQuery  string
	// pluginUninstall is the plugin asked to be removed, waiting for a
	// second click.
	pluginUninstall string
	pluginsScroll   ui.ScrollState

	// signInWeb is the plugin's login page, shown over the app.
	signInEntry *pluginEntry
	signInWeb   *mygo.WebView
	signInErr   string

	// The comments over a plugin's video, and whether they show.
	danmaku     []plugin.Cue
	danmakuOn   bool
	danmakuBusy bool

	// detailEpisodes are the parts or episodes a plugin lists for the
	// video its detail page shows.
	detailEpisodes []media.Item

	// The built-in WebDAV source: the server, what it listed, and where the
	// pages are.
	webdav          *webdav.Client
	webdavURL       string
	webdavUser      string
	webdavPass      string
	webdavPath      string
	webdavLoaded    string
	webdavHasLoaded bool
	webdavEntries   []webdav.Entry
	webdavVideos    []media.Item
	webdavBusy      bool
	webdavErr       string
	webdavScroll    ui.ScrollState
	webdavDirs      map[string]webdavDir
	webdavIndexAt   time.Time
	// webdavHLS says the video plays as HLS remuxed here, from
	// webdavBase on, and its length is what ffmpeg said.
	webdavHLS        bool
	webdavBase       time.Duration
	webdavDuration   time.Duration
	webdavDurationID string

	// The live source: the playlist read, its groups, and the group showing.
	iptvURL      string
	iptvChannels []iptv.Channel
	iptvGroups   []string
	iptvGroup    string
	iptvBusy     bool
	iptvLoaded   bool
	iptvErr      string
	iptvScroll   float32

	// The scan of every folder below the one showing.
	webdavAll      bool
	webdavScanning bool
	webdavScanned  int
	webdavFound    int
	webdavAllItems []media.Item
	webdavGen      int32

	// history and forward are the pages gone back from and forward to,
	// for the sidebar's arrows.
	history []page
	forward []page
	// focusSearch asks the search page to focus its field as it builds.
	focusSearch bool

	searchBox      ui.Handle
	homeScroll     ui.ScrollState
	libraryGrid    ui.GridState
	libraryScroll  ui.ScrollState
	detailScroll   ui.ScrollState
	settingsScroll ui.ScrollState

	player   *ui.VideoPlayer
	playing  media.Item
	volume   float64
	seek     float64
	seekedAt time.Time
	lastSave time.Time
	fresh    bool
	ended    bool

	// quality is what to ask a plugin for, and playingQuality what it gave.
	quality        string
	playingQuality string
	progressAt     time.Time

	// The pointer over the video, for the controls to come back at the
	// window's edges.
	pointerX, pointerY float32
	pointerIn          bool

	// The player's own state: subtitles read, speed, the episodes around
	// what plays, and whether the controls are out of the way.
	subs     []subtitle.Cue
	subsOn   bool
	rate     float64
	uiHidden bool
	uiShown  time.Time
	drawer   bool

	toast string
}

// Run runs the app until the user quits.
func Run() error {
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	a := &app{danmakuOn: true}
	mygo.App.WhenReady(a.start)
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.shutdown() })
	return mygo.App.Run()
}

func (a *app) start() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		dir = filepath.Join(os.TempDir(), "mygo-video")
	}
	st, err := store.Open(dir)
	if err != nil {
		log.Printf("store: %v", err)
		st, _ = store.Open(filepath.Join(os.TempDir(), "mygo-video"))
	}
	a.store = st
	a.dataDir = dir
	a.mark("ready", processStart)
	a.volume = st.Volume()
	roots := st.Roots()
	if len(roots) == 0 {
		roots = defaultRoots(dir)
		st.SetRoots(roots)
	}
	a.lib = local.New(roots)
	a.thumbDir = filepath.Join(dir, "thumbs")
	a.toolBin = ""
	if res, err := mygo.App.Path(mygo.PathResources); err == nil {
		a.toolBin = filepath.Join(res, "bin")
	}
	tools := st.Tools()
	a.ffmpeg = ffbin.Locate("ffmpeg", tools.FFmpeg, a.toolBin)
	a.thumbs = map[string]*ui.Bitmap{}
	a.thumbBusy = map[string]bool{}
	a.thumbUsed = map[string]time.Time{}
	a.thumbSem = make(chan struct{}, 2)
	a.posters = map[string]*ui.Bitmap{}
	a.posterBusy = map[string]bool{}
	a.posterUsed = map[string]time.Time{}
	a.settingTexts = map[string]*string{}
	a.pluginAccounts = map[string]*plugin.Account{}
	a.pluginAccountBusy = map[string]bool{}
	a.pluginChecked = map[string]bool{}
	a.webdavDirs = map[string]webdavDir{}

	a.proxy, _ = streamproxy.New()
	plugin.SetOpenURL(mygo.Shell.OpenExternal)
	plugin.SetAppVersion(mygo.App.Version())
	a.pluginsDir = filepath.Join(dir, "plugins")
	a.pluginDataDir = filepath.Join(dir, "plugin-data")
	_ = os.MkdirAll(a.pluginsDir, 0o700)
	a.rate = st.Rate()
	a.loadPlugins()
	a.loadPluginCookies()
	a.source = a.sourceIndex(st.Source())
	a.makeWebDAV()
	a.makeIPTV()
	// What the last run found shows at once, while the new scan goes on.
	if cached := st.Items(); len(cached) > 0 {
		var at time.Time
		if ms := st.ScannedAt(); ms > 0 {
			at = time.UnixMilli(ms)
		}
		a.lib.SetCached(cached, at)
	}

	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title:                appName,
		Width:                1180,
		Height:               780,
		MinWidth:             900,
		MinHeight:            600,
		StateKey:             "main",
		TitleBarStyle:        mygo.TitleBarHidden,
		TitleBarHeight:       38,
		TrafficLightPosition: &mygo.Point{X: 20, Y: 18},
		BackgroundColor:      "#0d0e11",
		Content:              ui.View(a.view),
	})
	a.mark("window", processStart)
	go a.scan()
}

// defaultRoots are the folders a new library looks in.
func defaultRoots(dataDir string) []string {
	videos := filepath.Join(dataDir, "videos")
	_ = os.MkdirAll(videos, 0o700)
	candidates := []string{videos}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "Movies"),
			filepath.Join(home, "Desktop"),
			filepath.Join(home, "Downloads"),
		)
	}
	var roots []string
	for _, dir := range candidates {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			roots = append(roots, dir)
		}
	}
	return roots
}

// scan walks the folders off the main thread, then shows what it found.
func (a *app) scan() {
	n := a.lib.Scan()
	mygo.RunOnMain(func() {
		if a.store != nil {
			a.store.SetItems(a.lib.Items(), time.Now().UnixMilli())
		}
		// Take the frames the cards will ask for, a few at a time.
		go a.pregenerateThumbs(a.lib.Items())
		a.mark("scan", processStart)
		if a.win != nil {
			a.win.Invalidate()
		}
		if n == 0 {
			a.toast = "没有找到视频。把文件放进 ~/Movies 或应用数据目录下的 videos 文件夹。"
		}
	})
}

// mark writes how long startup took to a log, in development only, so the
// cost of each step can be seen instead of guessed.
func (a *app) mark(step string, from time.Time) {
	if !mygo.IsDev() {
		return
	}
	ms := time.Since(from).Milliseconds()
	log.Printf("startup: %s at %dms", step, ms)
	if a.dataDir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(a.dataDir, "startup.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %-6s %dms\n", time.Now().Format("15:04:05.000"), step, ms)
}

func (a *app) shutdown() {
	a.saveProgress()
	if a.store != nil {
		a.store.SetVolume(a.volume)
		a.store.Save()
	}
}

func (a *app) invalidate() {
	if a.win != nil {
		a.win.Invalidate()
	}
}

func (a *app) goPage(p page) {
	if p == a.route {
		return
	}
	a.history = append(a.history, a.route)
	a.forward = nil
	a.route = p
	a.invalidate()
}

// back goes to the page before, as the sidebar's arrow does.
func (a *app) back() {
	if len(a.history) == 0 {
		return
	}
	a.forward = append(a.forward, a.route)
	a.route = a.history[len(a.history)-1]
	a.history = a.history[:len(a.history)-1]
	a.invalidate()
}

// goForward goes again to the page gone back from.
func (a *app) goForward() {
	if len(a.forward) == 0 {
		return
	}
	a.history = append(a.history, a.route)
	a.route = a.forward[len(a.forward)-1]
	a.forward = a.forward[:len(a.forward)-1]
	a.invalidate()
}

func (a *app) openDetail(it media.Item) {
	a.history = append(a.history, a.route)
	a.forward = nil
	a.current = it
	a.detailEpisodes = nil
	a.route = pageDetail
	a.detailScroll = ui.ScrollState{}
	if it.Source != "" && it.Source != "webdav" && it.Source != "iptv" {
		a.loadDetail(it)
	}
	a.invalidate()
}

// loadDetail asks a plugin for the parts of the video its page shows.
func (a *app) loadDetail(it media.Item) {
	p := a.pluginByID(it.Source)
	if p == nil || !p.Can("title") {
		return
	}
	cur := it.ID
	go func() {
		t, err := p.Title(it.RemoteID)
		mygo.RunOnMain(func() {
			if a.current.ID != cur || err != nil || t == nil {
				return
			}
			if t.Video.Name != "" {
				a.current.Name = t.Video.Name
			}
			if t.Video.Poster != "" {
				a.current.Poster = t.Video.Poster
			}
			if t.Video.Overview != "" {
				a.current.Overview = t.Video.Overview
			}
			a.detailEpisodes = nil
			for _, e := range t.Episodes {
				a.detailEpisodes = append(a.detailEpisodes, itemFromVideo(it.Source, e))
			}
			a.invalidate()
		})
	}()
}

// detailPlayTarget is the part to play from the detail page: the one
// showing, or the first a plugin listed.
func (a *app) detailPlayTarget(it media.Item) media.Item {
	if len(a.detailEpisodes) == 0 {
		return it
	}
	for _, ep := range a.detailEpisodes {
		if ep.ID == it.ID {
			return it
		}
	}
	return a.detailEpisodes[0]
}

// items is what the source being shown lists.
func (a *app) items() []media.Item {
	switch a.source {
	case sourceLocal:
		return a.lib.Items()
	case sourceWebDAV:
		return a.webdavItems()
	case sourceIPTV:
		return nil
	default:
		return a.pluginItems
	}
}

// sourceName is the source being shown.
func (a *app) sourceName() string {
	switch a.source {
	case sourceLocal:
		return "本地视频"
	case sourceWebDAV:
		return "WebDAV"
	case sourceIPTV:
		return "IPTV"
	}
	if i := a.source - sourcePluginBase; i >= 0 && i < len(a.plugins) {
		return a.plugins[i].man.Name
	}
	return "本地视频"
}

func (a *app) pluginByID(id string) *plugin.Plugin {
	for _, pe := range a.plugins {
		if pe.man.ID == id {
			return pe.p
		}
	}
	return nil
}

// switchSource shows another source and forgets what the last one listed.
func (a *app) switchSource(i int) {
	if i < 0 || i > len(a.plugins)+sourcePluginBase-1 || i == a.source {
		return
	}
	a.source = i
	a.sourceMenu = false
	a.pluginBrowse = nil
	a.pluginItems = nil
	a.pluginSearch = nil
	a.pluginErr = ""
	a.pluginTab = ""
	a.searchQuery = ""
	if i == sourceWebDAV {
		a.webdavPath = ""
		a.webdavLoaded = ""
		a.webdavHasLoaded = false
		a.webdavErr = ""
		a.webdavScanning = false
		a.webdavAll = len(a.webdavAllItems) > 0
		atomic.AddInt32(&a.webdavGen, 1)
	}
	if i == sourceIPTV {
		a.iptvGroup = ""
		a.iptvErr = ""
	}
	a.route = pageHome
	a.store.SetSource(a.sourceID())
	a.invalidate()
}

// loadPluginHome asks the source being shown for its home page, once.
func (a *app) loadPluginHome() {
	a.loadPluginTab()
}

// loadPluginTab asks the plugin for the tab showing: its home, or the
// library it keeps.
func (a *app) loadPluginTab() {
	if a.source < sourcePluginBase || a.pluginBusy || a.pluginBrowse != nil {
		return
	}
	pe := a.plugins[a.source-sourcePluginBase]
	p := pe.p
	if p == nil {
		return
	}
	id := pe.man.ID
	tab := a.pluginTab
	a.pluginBusy = true
	go func() {
		var b *media.Browse
		var err error
		switch tab {
		case "history":
			b, err = p.History()
		case "favorites":
			b, err = p.Favorites()
		case "following":
			b, err = p.Following()
		case "watchlater":
			b, err = p.WatchLater()
		case "live":
			b, err = p.LiveList()
		default:
			b, err = p.Home()
		}
		mygo.RunOnMain(func() {
			a.pluginBusy = false
			if a.source < sourcePluginBase || a.plugins[a.source-sourcePluginBase].man.ID != id || a.pluginTab != tab {
				return
			}
			if err != nil {
				a.pluginErr = err.Error()
				a.invalidate()
				return
			}
			a.pluginErr = ""
			a.pluginBrowse = b
			a.pluginItems = nil
			for _, sec := range b.Sections {
				for _, v := range sec.Videos {
					a.pluginItems = append(a.pluginItems, itemFromVideo(id, v))
				}
			}
			a.invalidate()
		})
	}()
}

// setPluginTab shows another of the plugin's pages.
func (a *app) setPluginTab(tab string) {
	if a.pluginTab == tab {
		return
	}
	a.pluginTab = tab
	a.pluginBrowse = nil
	a.pluginItems = nil
	a.pluginErr = ""
	a.pluginBusy = false
	a.loadPluginTab()
	a.invalidate()
}

// pluginTabs are the pages a plugin offers beyond its home.
func (a *app) pluginTabs() []struct{ key, label string } {
	tabs := []struct{ key, label string }{{key: "", label: "推荐"}}
	if a.source < sourcePluginBase {
		return tabs
	}
	p := a.plugins[a.source-sourcePluginBase].p
	if p == nil {
		return tabs
	}
	for _, t := range []struct{ key, label, export string }{
		{"live", "直播", "live"},
		{"history", "历史", "history"},
		{"favorites", "收藏", "favorites"},
		{"following", "追番", "following"},
		{"watchlater", "稍后再看", "watchLater"},
	} {
		if p.Can(t.export) {
			tabs = append(tabs, struct{ key, label string }{t.key, t.label})
		}
	}
	return tabs
}

// itemFromVideo is a video a plugin listed, as the pages show it.
func itemFromVideo(source string, v media.Video) media.Item {
	return media.Item{
		ID:       source + ":" + v.ID,
		RemoteID: v.ID,
		Source:   source,
		Name:     v.Name,
		Series:   v.Series,
		Season:   v.Season,
		Episode:  v.Episode,
		Duration: v.Duration,
		Poster:   v.Poster,
		Overview: v.Overview,
		URL:      v.URL,
		Live:     v.Live,
	}
}

// play opens an item, from the start or where the listener stopped.
func (a *app) play(it media.Item, fromStart bool) {
	switch {
	case it.Source == "webdav":
		a.playWebDAV(it, fromStart)
		return
	case it.Source != "":
		a.playPlugin(it, fromStart)
		return
	}
	a.closePlayer()
	p, err := video.Open(it.Path)
	if err != nil {
		a.toast = "打不开这个视频：" + err.Error()
		a.openDetail(it)
		return
	}
	a.subs = subtitle.Load(it.Path)
	a.subsOn = len(a.subs) > 0
	a.beginPlayback(it, p, fromStart)
}

// playPlugin asks the plugin for a stream and plays it through the proxy,
// or opens the page where the plugin plays it itself.
func (a *app) playPlugin(it media.Item, fromStart bool) {
	p := a.pluginByID(it.Source)
	if p == nil {
		a.say("这个来源已经不在了")
		return
	}
	go func() {
		pb, err := p.Play(it.RemoteID, a.quality)
		mygo.RunOnMain(func() { a.openPlayback(it, pb, err, fromStart) })
	}()
}

func (a *app) openPlayback(it media.Item, pb *media.Playback, err error, fromStart bool) {
	if err != nil {
		switch {
		case errors.Is(err, plugin.ErrDRM):
			a.say("受 DRM 保护，需要在官方 App 播放")
		case errors.Is(err, plugin.ErrSignIn):
			a.say("需要先登录这个来源")
		case errors.Is(err, plugin.ErrSetup):
			a.say("这个来源还需要设置")
		case errors.Is(err, plugin.ErrUnavailable):
			a.say("这个视频暂时不可用")
		default:
			a.say("%v", err)
		}
		return
	}
	if pb.Open != "" {
		if err := mygo.Shell.OpenExternal(pb.Open); err != nil {
			a.say("%v", err)
			return
		}
		a.say("已在浏览器打开")
		return
	}
	videoURL, audio, kind := pb.URL, "", pb.Type
	if pb.Dash != nil && pb.Dash.Video != "" {
		videoURL, audio, kind = pb.Dash.Video, pb.Dash.Audio, "dash"
	}
	if p := a.pluginByID(it.Source); p != nil {
		if !p.AllowsHost(videoURL) || (audio != "" && !p.AllowsHost(audio)) {
			a.say("这个来源没有访问该地址的权限")
			return
		}
	}
	a.closePlayer()
	url, token, err := a.proxy.Open(streamproxy.Stream{
		URL:     videoURL,
		Type:    kind,
		Audio:   audio,
		Headers: pb.Headers,
	})
	if err != nil {
		a.say("%v", err)
		return
	}
	player, err := video.OpenURL(url)
	if err != nil {
		a.proxy.Forget(token)
		a.say("%v", err)
		return
	}
	a.proxyToken = token
	a.subs = nil
	a.subsOn = false
	if pb.Live {
		fromStart = true
	}
	a.beginPlayback(it, player, fromStart)
	a.playingQuality = pb.Quality
	if pb.Live {
		a.startLiveDanmaku(it)
	} else {
		a.loadDanmaku(it)
	}
}

// beginPlayback makes p the video showing, from where the listener stopped
// unless asked to start over.
func (a *app) beginPlayback(it media.Item, p *ui.VideoPlayer, fromStart bool) {
	if it.Source != "webdav" {
		a.webdavHLS = false
		a.webdavBase = 0
		a.webdavDuration = 0
		a.webdavDurationID = ""
	}
	a.player = p
	a.playing = it
	a.playingQuality = ""
	a.danmaku = nil
	a.fresh = fromStart
	a.route = pagePlayer
	a.rate = a.store.Rate()
	a.drawer = false
	a.ended = false
	a.uiHidden = false
	a.uiShown = time.Now()
	a.seekedAt = time.Time{}
	p.SetRate(a.rate)
	p.SetVolume(a.volume)
	if !fromStart {
		if prog, ok := a.store.Progress(it.ID); ok && prog.Started() {
			p.Seek(time.Duration(prog.Position) * time.Millisecond)
		}
	}
	p.Play()
	a.lastSave = time.Now()
	a.invalidate()
}

func (a *app) leavePlayer() {
	a.closePlayer()
	a.seek = 0
	a.drawer = false
	a.uiHidden = false
	if a.playing.Source == "iptv" {
		a.route = pageHome
	} else {
		a.route = pageDetail
	}
	a.invalidate()
}

func (a *app) closePlayer() {
	if a.proxyToken != "" && a.proxy != nil {
		a.proxy.Forget(a.proxyToken)
		a.proxyToken = ""
	}
	if a.player == nil {
		return
	}
	a.saveProgress()
	a.stopLiveDanmaku()
	a.player.Close()
	a.player = nil
}

// saveProgress writes where the video stands, at most what changed.
func (a *app) saveProgress() {
	p := a.player
	if p == nil || a.playing.ID == "" {
		return
	}
	// A live channel has no place to keep.
	if a.playing.Source == "iptv" || a.playing.Live {
		return
	}
	// Before the first picture the engine has no position to keep, and a
	// zero there would throw away where the listener actually was.
	if !p.Ready() {
		return
	}
	pos := a.playhead().Milliseconds()
	dur := a.playLength().Milliseconds()
	done := dur > 0 && pos >= dur-1500
	prev, ok := a.store.Progress(a.playing.ID)
	if ok && prev.Started() && !a.fresh && pos < 1000 && !done {
		return
	}
	if ok && prev.Done && done {
		return
	}
	if ok && prev.Position == pos && prev.Done == done {
		return
	}
	a.store.SetProgress(a.playing.ID, media.Progress{
		Position: pos,
		Duration: dur,
		Done:     done,
		Updated:  time.Now().UnixMilli(),
	})
	if a.playing.Source != "" {
		if p := a.pluginByID(a.playing.Source); p != nil && p.Can("progress") {
			if done || time.Since(a.progressAt) >= 15*time.Second {
				a.progressAt = time.Now()
				id := a.playing.RemoteID
				go p.Progress(id, pos, dur, done)
			}
		}
	}
}

// tick keeps the stored progress fresh while a video plays.
func (a *app) tick() {
	if a.player == nil {
		return
	}
	if time.Since(a.lastSave) >= 5*time.Second {
		a.saveProgress()
		a.lastSave = time.Now()
	}
}

func (a *app) setVolume(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	a.volume = v
	if a.player != nil {
		a.player.SetVolume(v)
	}
}

func (a *app) say(format string, args ...any) {
	a.toast = fmt.Sprintf(format, args...)
	a.invalidate()
}
