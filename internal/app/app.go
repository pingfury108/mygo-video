package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"

	"mygo-video/internal/executil"
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
	// dataDirCustom says the listener chose where the data lives.
	dataDirCustom bool
	// dataDirBusy locks playback and quitting while the data directory is
	// being copied and switched. dataDirQuitAllowed lets the relaunch after
	// a successful copy through OnBeforeQuit while that lock stays in place.
	dataDirBusy        bool
	dataDirQuitAllowed bool
	dataDirStatus      string
	// retry is the stream to open again through ffmpeg when the player
	// turns the first one down, and retryStage how far that went for an item:
	// 0 untried, 1 remuxed, 2 transcoded.
	retryStream  streamproxy.Stream
	retryStage   map[string]int
	retryPending string
	// hevcExt is whether Microsoft's HEVC decoder is installed, checked once.
	hevcExt        bool
	hevcExtChecked bool
	// hevcProbe remembers what ffmpeg said of a stream, so a retry does
	// not open it again over the network.
	hevcProbe map[string]bool
	// webdavGPUFailed says the GPU decoder did not open a video of the
	// server: the ways in after it are tried for this item, and a drag
	// does not open it again on every seek.
	webdavGPUFailed map[string]bool
	// playSeq numbers the play requests a plugin answers apart: one that
	// took its time must not open over the video chosen after it.
	playSeq int
	// detailGen numbers the detail pages a plugin answers apart.
	detailGen int
	// errLogged is the item whose player error is already in the log.
	errLogged string

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

	// The official plugin market: its list, what is being installed, and
	// how far the dialog is scrolled.
	marketDialog     bool
	marketLoading    bool
	marketFetched    bool
	marketErr        string
	marketInstalling string
	marketPlugins    []marketPlugin
	marketScroll     ui.ScrollState

	// What the source being shown has: its home page, the videos of it,
	// the search last run, and the error that stopped them.
	pluginBrowse *media.Browse
	pluginItems  []media.Item
	pluginSearch []media.Item
	pluginErr    string
	pluginBusy   bool
	pluginTab    string
	pluginGen    int
	searchQuery  string
	searchBusy   bool
	searchGen    int

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
	// danmakuPrefs is how they look, kept in the store.
	danmakuPrefs store.DanmakuPrefs

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
	webdavHLS  bool
	webdavBase time.Duration
	// eventHLS says the player shows a proxy-made event playlist, whose
	// own duration is only what ffmpeg has written so far. streamDuration
	// is the file's real length, from ffmpeg, for the item it names.
	eventHLS         bool
	streamDuration   time.Duration
	streamDurationID string

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

	// The phone shell: its tab, the sheets it shows, and the native player.
	// Desktop pages keep using the fields around these untouched.
	mobileTab          int
	mobileFilter       int
	mobileSearchSource int
	mobileScrolls      [4]ui.ScrollState
	mobilePlayerScroll ui.ScrollState
	mobileWebdavSheet  bool
	mobileEpisodes     bool
	mobileImportBusy   bool
	mobileImportText   string
	mobileSearchIndex  bool
	mobilePanscan      float64
	mobile             *mobilePlayback

	searchBox       ui.Handle
	homeScroll      ui.ScrollState
	libraryScroll   ui.ScrollState
	searchScroll    ui.ScrollState
	webdavAllScroll ui.ScrollState
	detailScroll    ui.ScrollState
	settingsScroll  ui.ScrollState

	player  *ui.VideoPlayer
	playing media.Item
	volume  float64
	// avOffset moves the picture against the sound for this machine's
	// output; avTune is the engine playing now, if it can be moved.
	avOffset     time.Duration
	avTune       avTuner
	seek         float64
	seekedAt     time.Time
	seekDragging bool
	seekDrag     bool
	seekHold     bool
	lastSave     time.Time
	fresh        bool
	ended        bool
	// mpvPlaying says an mpv window outside this one plays a video, whose
	// screen must be kept on too.
	mpvPlaying bool
	mpvProcess *os.Process

	// quality is what to ask a plugin for, and playingQuality what it gave.
	quality        string
	playingQuality string
	// playStarted is when the stream was asked for, and playReady whether
	// its first frame came.
	playStarted time.Time
	playReady   bool
	progressAt  time.Time

	// The pointer over the video, for the controls to come back at the
	// window's edges.
	pointerX, pointerY float32
	pointerIn          bool

	// displayLock keeps the screen on while video plays. It holds one
	// request across playback, so a pause does not churn power requests.
	displayLock func()
	displaySeen bool

	// The player's own state: subtitles read, speed, the episodes around
	// what plays, and whether the controls are out of the way.
	subs     []subtitle.Cue
	subsOn   bool
	rate     float64
	uiHidden bool
	uiShown  time.Time
	drawer   bool

	toast   string
	toastAt time.Time
}

// ReportFatal keeps what stopped the app from running in the data directory:
// a windowed program on Windows has no terminal to print it to.
func ReportFatal(err error) {
	dir, derr := mygo.App.Path(mygo.PathUserData)
	if derr != nil {
		dir = os.TempDir()
	}
	line := fmt.Sprintf("%s  %v\n", time.Now().Format("2006-01-02 15:04:05"), err)
	_ = os.WriteFile(filepath.Join(dir, "startup-error.log"), []byte(line), 0o600)
	os.Exit(1)
}

// Run runs the app until the user quits.
func Run() error {
	_, custom, err := prepareBaseDirs()
	if err != nil {
		log.Printf("data dir: %v", err)
	}
	baseDirCustom = custom
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	// The update window Sparkle made familiar, in native UI: it checks
	// the GitHub releases of the app and installs a newer one.
	if !isPhone() {
		mygo.Use(native.Plugin)
		setupMenu()
	}
	a := &app{danmakuOn: true}
	// A second launch hands its command line over and exits; bring the
	// window the first one holds back to the front, or the launch looks
	// like nothing happened.
	mygo.App.OnSecondInstance(func(args []string, wd string) {
		mygo.RunOnMain(func() {
			if a.win == nil {
				return
			}
			a.win.Show()
			a.win.Focus()
		})
	})
	mygo.App.WhenReady(a.start)
	if isPhone() {
		mygo.Power.OnSuspend(func() {
			mygo.RunOnMain(func() {
				if a.mobile == nil {
					return
				}
				a.mobile.Pause()
				a.saveProgress()
				a.releaseDisplayLock()
				a.invalidate()
			})
		})
	}
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		// Saving here while a copy is being verified would change state.json
		// under the copy: refuse the quit before touching any data.
		if a.dataDirBusy && !a.dataDirQuitAllowed {
			e.PreventDefault()
			a.say("正在复制数据，完成前不能退出")
			return
		}
		a.shutdown()
	})
	mygo.App.OnQuitCanceled(func() {
		if !a.dataDirQuitAllowed {
			return
		}
		// The pointer already names the new directory, so playback must stay
		// locked, but the listener can quit again to let the switch take hold.
		a.dataDirStatus = "重启被取消，请退出应用后再打开"
		a.invalidate()
	})
	return mygo.App.Run()
}

// setupMenu gives the app a menu holding the update check: macOS puts it in
// the app menu, Windows and Linux in a menu bar.
func setupMenu() {
	appMenu := []*mygo.MenuItem{
		updater.MenuItem(),
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	}
	if runtime.GOOS == "darwin" {
		appMenu = []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			updater.MenuItem(),
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: appName, Submenu: appMenu},
	}))
}

func (a *app) start() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		dir = baseDir()
	}
	if dataDirRecovered {
		recoverNestedState(dir, defaultBaseDirPath())
	}
	st, err := store.Open(dir)
	if err != nil {
		log.Printf("store: %v", err)
		if dir != baseDir() {
			dir = baseDir()
			st, err = store.Open(dir)
		}
	}
	if err != nil {
		ReportFatal(fmt.Errorf("store: %w", err))
	}
	a.store = st
	a.dataDir = dir
	a.dataDirCustom = baseDirCustom
	a.finishDataDirMove()
	a.mark("ready", processStart)
	a.volume = st.Volume()
	a.avOffset = st.AVOffset()
	a.danmakuPrefs = st.Danmaku()
	roots := st.Roots()
	if len(roots) == 0 {
		roots = defaultRoots(dir)
		st.SetRoots(roots)
	} else if baseDirCustom {
		// A move should not leave library roots pointing into the old
		// default tree, where the app no longer keeps anything.
		fixed := recoveredRoots(roots, nestedDataDirs(defaultBaseDirPath()), dir)
		if !sameRoots(roots, fixed) {
			roots = fixed
			st.SetRoots(roots)
		}
	}
	a.lib = local.New(roots)
	a.thumbDir = filepath.Join(dir, "thumbs")
	a.toolBin = ""
	if res, err := mygo.App.Path(mygo.PathResources); err == nil {
		a.toolBin = filepath.Join(res, "bin")
	}
	tools := st.Tools()
	a.ffmpeg = ffbin.Locate("ffmpeg", tools.FFmpeg, a.toolBin)
	a.diag("ffmpeg=%q toolBin=%q", a.ffmpeg, a.toolBin)
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
	streamproxy.Log = func(msg string) { a.diag("%s", msg) }
	streamproxy.TempDir = a.tempDir()
	// What a run that ended without closing its streams left behind.
	for _, prefix := range []string{"mygo-video-hls-*", "mygo-video-plugin-*"} {
		if old, err := filepath.Glob(filepath.Join(streamproxy.TempDir, prefix)); err == nil {
			for _, d := range old {
				_ = os.RemoveAll(d)
			}
		}
	}
	a.retryStage = map[string]int{}
	a.hevcProbe = map[string]bool{}
	a.webdavGPUFailed = map[string]bool{}
	a.diag("version=%s os=%s/%s", mygo.App.Version(), runtime.GOOS, runtime.GOARCH)
	go a.logTools()
	plugin.SetOpenURL(mygo.Shell.OpenExternal)
	plugin.SetAppVersion(mygo.App.Version())
	a.pluginsDir = filepath.Join(dir, "plugins")
	a.pluginDataDir = filepath.Join(dir, "plugin-data")
	_ = os.MkdirAll(a.pluginsDir, 0o700)
	a.rate = st.Rate()
	if isPhone() {
		// The phone build is the two sources it has a player for. Third-party
		// plugins and live playlists stay on the desktop pages.
		a.plugins = nil
	} else {
		a.loadPlugins()
		a.loadPluginCookies()
	}
	a.source = a.sourceIndex(st.Source())
	a.makeWebDAV()
	if !isPhone() {
		a.makeIPTV()
	}
	// What the last run found shows at once, while the new scan goes on.
	if cached := st.Items(); len(cached) > 0 {
		var at time.Time
		if ms := st.ScannedAt(); ms > 0 {
			at = time.UnixMilli(ms)
		}
		a.lib.SetCached(cached, at)
	}

	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title:          appName,
		Width:          1180,
		Height:         780,
		MinWidth:       900,
		MinHeight:      600,
		StateKey:       "main",
		TitleBarStyle:  mygo.TitleBarHidden,
		TitleBarHeight: windowButtonHeight,
		// Windows and Linux draw the buttons of a hidden title bar in a
		// color the dark background hides, so the app draws its own.
		HideWindowButtons:    runtime.GOOS != "darwin",
		TrafficLightPosition: &mygo.Point{X: 20, Y: 18},
		BackgroundColor:      "#0d0e11",
		Content:              ui.View(a.view),
	})
	// The close button destroys the window before the quit listeners run:
	// keep it, or a copy would leave the app running without a window.
	a.win.OnClose(func(e *mygo.CloseEvent) {
		if a.dataDirBusy && !a.dataDirQuitAllowed {
			e.PreventDefault()
			a.say("正在复制数据，完成前不能退出")
		}
	})
	if isPhone() {
		mygo.SystemBars.SetStyle(mygo.BarStyleLight)
		mygo.SystemBars.SetHidden(false)
	}
	a.mark("window", processStart)
	go a.scan()
}

// defaultRoots are the folders a new library looks in.
func defaultRoots(dataDir string) []string {
	videos := filepath.Join(dataDir, "videos")
	_ = os.MkdirAll(videos, 0o700)
	if isPhone() {
		// A phone cannot read the desktop home folders, and the picker hands
		// copies to the app: the library is the app's own videos folder.
		return []string{videos}
	}
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
		// The data directory is being copied and verified: a scan result
		// saved now would rewrite state.json under the copy.
		if a.dataDirBusy {
			return
		}
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
			a.say("没有找到视频。把文件放进 ~/Movies 或应用数据目录下的 videos 文件夹。")
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
	a.detailGen++
	a.route = pageDetail
	a.detailScroll = ui.ScrollState{}
	a.mobilePlayerScroll = ui.ScrollState{}
	a.mobileEpisodes = false
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
	gen := a.detailGen
	go func() {
		t, err := p.Title(it.RemoteID)
		mygo.RunOnMain(func() {
			if a.detailGen != gen || a.current.ID != cur || err != nil || t == nil {
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
	a.pluginBusy = false
	a.pluginGen++
	a.searchQuery = ""
	a.searchBusy = false
	a.searchGen++
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
	if a.source < sourcePluginBase || a.pluginBusy || a.pluginBrowse != nil || a.pluginErr != "" {
		return
	}
	i := a.source - sourcePluginBase
	if i >= len(a.plugins) {
		return
	}
	pe := a.plugins[i]
	p := pe.p
	if p == nil {
		return
	}
	id := pe.man.ID
	tab := a.pluginTab
	gen := a.pluginGen
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
			if a.pluginGen != gen {
				return
			}
			a.pluginBusy = false
			i := a.source - sourcePluginBase
			if i < 0 || i >= len(a.plugins) || a.plugins[i].man.ID != id || a.pluginTab != tab {
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
	a.pluginGen++
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
	if a.dataDirBusy {
		a.say("正在复制数据，暂时不能播放")
		return
	}
	a.playSeq++
	// A play chosen again is tried the whole way in again, the GPU first.
	delete(a.webdavGPUFailed, it.ID)
	delete(a.retryStage, it.ID)
	a.retryPending = ""
	switch {
	case it.Source == "webdav":
		a.playWebDAV(it, fromStart)
		return
	case it.Source != "":
		if isPhone() {
			a.say("手机版只支持本地视频和 WebDAV")
			return
		}
		a.playPlugin(it, fromStart)
		return
	}
	if isPhone() {
		a.playMobileLocal(it, fromStart)
		return
	}
	a.closePlayer()
	// AVFoundation turns down the containers ffmpeg writes, such as MKV.
	// They go through ffmpeg before the player sees one, rather than
	// waiting for it to fail.
	if needsFFmpegBeforeOpen(it.Path) && a.playRemuxed(it, fromStart) {
		return
	}
	p, err := video.Open(it.Path)
	if err != nil {
		// Media Foundation, on Windows, turns down some containers and
		// codecs; ffmpeg can remux most of them into HLS it takes.
		if a.playRemuxed(it, fromStart) {
			return
		}
		a.say("打不开这个视频：%v", err)
		a.openDetail(it)
		return
	}
	a.subs = subtitle.Load(it.Path)
	a.subsOn = len(a.subs) > 0
	if len(a.subs) == 0 && a.ffmpeg != "" {
		a.extractSubs(it)
	}
	a.retryStream = streamproxy.Stream{URL: it.Path}

	a.beginPlayback(it, p, fromStart)
}

// extractSubs takes the first subtitle track ffmpeg finds inside the video,
// for files that carry one instead of a file beside them.
func (a *app) extractSubs(it media.Item) {
	ffmpeg, id, path := a.ffmpeg, it.ID, it.Path
	go func() {
		cues := subtitle.Extract(path, ffmpeg)
		if len(cues) == 0 {
			return
		}
		mygo.RunOnMain(func() {
			if a.playing.ID != id || len(a.subs) > 0 {
				return
			}
			a.subs = cues
			a.subsOn = true
			a.invalidate()
		})
	}()
}

// openStream plays a stream through the proxy. When the system's player
// turns the URL down, which Media Foundation does for some containers and
// codecs, ffmpeg remuxes it into HLS and the player takes that.
func (a *app) openStream(s streamproxy.Stream) (*ui.VideoPlayer, string, string, error) {
	if a.hevcNeedsTranscode(s) {
		s.Type = "transcode"
		a.diag("hevc: transcoding %s", s.URL)
	}
	if s.Type == "transcode" && s.VideoEncoder == "" {
		s.VideoEncoder = a.pickVideoEncoder()
	}
	a.diag("open stream type=%s url=%s", s.Type, s.URL)
	url, token, err := a.proxy.Open(s)
	if err != nil {
		a.diag("open stream failed: %v", err)
		return nil, "", "", err
	}

	player, err := video.OpenURL(url)
	if err == nil {
		return player, token, url, nil
	}
	a.proxy.Forget(token)
	if s.FFmpeg == "" {
		return nil, "", "", err
	}
	s.Type = "remux"
	url, token, err = a.proxy.Open(s)
	if err != nil {
		return nil, "", "", err
	}
	player, err = video.OpenURL(url)
	if err != nil {
		a.proxy.Forget(token)
		return nil, "", "", err
	}
	a.diag("remuxed %s", s.URL)
	return player, token, url, nil
}

// playRemuxed hands a file the system's player refuses to ffmpeg, which
// remuxes it into HLS the player takes: Media Foundation turns down some
// containers and audio codecs it would otherwise play.
func (a *app) playRemuxed(it media.Item, fromStart bool) bool {
	if a.ffmpeg == "" || a.proxy == nil {
		return false
	}
	kind := "remux"
	if a.hevcNeedsTranscode(streamproxy.Stream{URL: it.Path}) {
		kind = "transcode"
		a.diag("hevc: transcoding %s", it.Path)
	}
	encoder := ""
	if kind == "transcode" {
		encoder = a.pickVideoEncoder()
	}
	tagHEVC := runtime.GOOS == "darwin" &&
		a.hevcSource(streamproxy.Stream{URL: it.Path})
	url, token, err := a.proxy.Open(streamproxy.Stream{
		URL:          it.Path,
		Type:         kind,
		VideoEncoder: encoder,
		FFmpeg:       a.ffmpeg,
		TagHEVC:      tagHEVC,
	})
	if err != nil {
		return false
	}
	player, err := video.OpenURL(url)
	if err != nil {
		a.proxy.Forget(token)
		return false
	}
	a.proxyToken = token
	a.subs = subtitle.Load(it.Path)
	a.subsOn = len(a.subs) > 0
	if len(a.subs) == 0 {
		a.extractSubs(it)
	}
	a.retryStream = streamproxy.Stream{URL: it.Path, Type: "remux"}
	a.beginPlayback(it, player, fromStart)
	a.eventHLS = true
	a.probeStreamDuration(it.ID, it.Path, nil)
	a.diag("remuxed %s", it.Path)
	return true
}

// playPlugin asks the plugin for a stream and plays it through the proxy,
// or opens the page where the plugin plays it itself.
func (a *app) playPlugin(it media.Item, fromStart bool) {
	p := a.pluginByID(it.Source)
	if p == nil {
		a.say("这个来源已经不在了")
		return
	}
	seq := a.playSeq
	go func() {
		pb, err := p.Play(it.RemoteID, a.quality)
		mygo.RunOnMain(func() {
			// The listener chose another video while this plugin took
			// its time: the answer belongs to a playback left behind.
			if a.playSeq != seq {
				return
			}
			a.openPlayback(it, pb, err, fromStart)
		})
	}()
}

func (a *app) openPlayback(it media.Item, pb *media.Playback, err error, fromStart bool) {
	if a.dataDirBusy {
		return
	}
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
		u, err := url.Parse(pb.Open)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
			a.say("这个来源给了一个打不开的地址")
			return
		}
		p := a.pluginByID(it.Source)
		if p == nil {
			a.say("这个来源已经不在了")
			return
		}
		if !p.AllowsStreamHost(pb.Open) && !p.AllowsHost(pb.Open) {
			a.say("这个来源没有打开该地址的权限")
			return
		}
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
	a.diag("play source=%s type=%s live=%v ffmpeg=%q", it.Source, kind, pb.Live, a.ffmpeg)
	if p := a.pluginByID(it.Source); p != nil {
		if !p.AllowsStreamHost(videoURL) || (audio != "" && !p.AllowsStreamHost(audio)) {
			a.say("这个来源没有访问该地址的权限")
			return
		}
	}
	a.closePlayer()
	if (kind == "dash" || kind == "flv") && a.ffmpeg == "" {
		a.say("这个流需要 ffmpeg：设置 → 工具")
		return
	}
	// A file plays here, its picture kept on the GPU; mpv takes it where that
	// fails, in a window of its own.
	if !pb.Live && kind != "dash" && kind != "flv" {
		if p, ok := a.gpuPlayer(it); ok {
			a.beginPlayback(it, p, fromStart)
			return
		}
		if a.playMPV(it, 0) {
			return
		}
	}
	player, token, url, err := a.openStream(streamproxy.Stream{
		URL:     videoURL,
		Type:    kind,
		Audio:   audio,
		Headers: pb.Headers,
		FFmpeg:  a.ffmpeg,
		Relay:   pb.Relay,
	})
	if err != nil {
		a.say("%v", err)
		return
	}
	a.proxyToken = token
	a.subs = nil
	a.subsOn = false
	if pb.Live {
		fromStart = true
	}
	a.retryStream = streamproxy.Stream{URL: videoURL, Audio: audio, Headers: pb.Headers, Relay: pb.Relay}
	a.beginPlayback(it, player, fromStart)
	if !pb.Live && strings.Contains(url, "/hls/") {
		a.eventHLS = true
		a.probeStreamDuration(it.ID, videoURL, pb.Headers)
	}
	a.playingQuality = pb.Quality
	if pb.Live {
		a.startLiveDanmaku(it)
	} else {
		a.loadDanmaku(it)
	}
	go func() {
		time.Sleep(20 * time.Second)
		mygo.RunOnMain(func() {
			if a.player == nil || a.playing.ID != it.ID || a.player.Ready() {
				return
			}
			a.say("这个流打不开，地址可能已过期")
			a.invalidate()
		})
	}()
}

// canRetry says the player refused what it got, and ffmpeg can still try a
// remux, then a transcode. It only reads the state: the view calls it every
// frame, and changing the state there started several retries at once.
func (a *app) canRetry() bool {
	if a.ffmpeg == "" || a.player == nil || a.playing.ID == "" {
		return false
	}
	if a.route != pagePlayer || a.retryStream.URL == "" ||
		a.retryPending == a.playing.ID || a.retryStage[a.playing.ID] >= 2 {
		return false
	}
	return true
}

// scheduleRetry starts the next way in once, for the video still playing.
// The stage moves when the retry is really chosen, not while the view reads
// the error every frame.
func (a *app) scheduleRetry() {
	if !a.canRetry() {
		return
	}
	id, seq := a.playing.ID, a.playSeq
	a.retryStage[id]++
	a.retryPending = id
	time.AfterFunc(80*time.Millisecond, func() {
		mygo.RunOnMain(func() {
			if a.retryPending != id {
				return
			}
			a.retryPending = ""
			if a.playSeq != seq || a.playing.ID != id ||
				a.route != pagePlayer || a.player == nil {
				return
			}
			a.retryPlayback()
		})
	})
}

// reconnectPlayback retries a video that played and then broke off, from
// the place it stood. The first way in is the one that worked before any
// retry: the stream may have recovered while it was open.
func (a *app) reconnectPlayback() {
	if a.player == nil || a.playing.ID == "" || a.route != pagePlayer {
		return
	}
	id := a.playing.ID
	a.retryPending = ""
	if a.retryStage[id] < 1 {
		a.retryStage[id] = 1
	}
	a.retryPlayback()
}

// retryPlayback plays the stream again through ffmpeg: remuxed first, then
// transcoded to H.264, for a file the system's player has no decoder for.
func (a *app) retryPlayback() {
	if a.route != pagePlayer || a.player == nil || a.playing.ID == "" {
		return
	}
	s := a.retryStream
	resume := a.playhead()
	// The GPU decoder failed on this file: mpv is the next way in, and
	// the one known to keep a 4K picture with its sound, when this build
	// ships it. It takes the file in a window of its own.
	if a.avTune != nil && a.playMPV(a.playing, resume) {
		if a.playing.Source == "webdav" {
			// A drag while this plays asks for the place in the file; the
			// GPU is not tried again on every one of them.
			a.webdavGPUFailed[a.playing.ID] = true
		}
		// mpv plays in a window of its own: this window goes back to the
		// page the video was chosen from, rather than draw a player that
		// has none.
		a.leavePlayer()
		return
	}
	webdav := a.playing.Source == "webdav"
	if webdav {
		a.webdavGPUFailed[a.playing.ID] = true
		if a.playReady {
			// A video that played and broke off goes on where it stood.
			// One that never showed a picture keeps the place it was
			// asked for, which a resume from history may already be.
			s.Start = resume
		}
	}
	if a.retryStage[a.playing.ID] >= 2 {
		s.Type = "transcode"
		s.VideoEncoder = a.pickVideoEncoder()
	} else {
		s.Type = "remux"
	}
	if runtime.GOOS == "darwin" && s.Type == "remux" &&
		a.hevcSource(streamproxy.Stream{URL: s.URL, Headers: s.Headers}) {
		// AVFoundation reads HEVC only from the hvcC box an fMP4 remux
		// writes, not from the hev1 tag the source may carry.
		s.TagHEVC = true
	}
	s.FFmpeg = a.ffmpeg
	it := a.playing
	live := it.Live || it.Source == "iptv"
	ready := a.playReady
	fromStart := a.fresh
	if ready && !live {
		fromStart = false
	}
	url, token, err := a.proxy.Open(s)
	if err != nil {
		a.say("换一种方式也打不开：%v", err)
		return
	}
	player, err := video.OpenURL(url)
	if err != nil {
		a.proxy.Forget(token)
		a.say("%v", err)
		return
	}
	a.closePlayer()
	a.retryStream = s
	a.proxyToken = token
	if webdav {
		// The remux begins at s.Start, and the player's own clock counts
		// from there: the playhead is put back on the file's timeline.
		a.webdavHLS = true
		a.webdavBase = s.Start
		a.beginPlayback(it, player, true)
		if !live {
			a.eventHLS = true
			a.probeStreamDuration(it.ID, s.URL, s.Headers)
		}
	} else {
		a.beginPlayback(it, player, fromStart)
		if !live {
			a.eventHLS = true
			a.probeStreamDuration(it.ID, s.URL, s.Headers)
		}
		if ready && !live {
			// The player's clock starts at zero for the remux: put it
			// back where the interrupted playback stood.
			player.Seek(resume)
			a.seek = resume.Seconds()
			a.seekHold = true
			a.seekedAt = time.Now()
		}
	}
	a.diag("retried with %s: %s", s.Type, s.URL)
}

func (a *app) beginPlayback(it media.Item, p *ui.VideoPlayer, fromStart bool) {
	if a.dataDirBusy {
		if p != nil {
			p.Close()
		}
		a.say("正在复制数据，暂时不能播放")
		return
	}
	// A caller that already plays the video on the GPU passes its player; one
	// that has the system's player gets the GPU one here, where it takes the
	// codec.
	if p == nil {
		if g, ok := a.gpuPlayer(it); ok {
			p = g
		}
	}
	if it.Source != "webdav" {
		a.webdavHLS = false
		a.webdavBase = 0
	}
	a.eventHLS = false
	if a.streamDurationID != it.ID {
		a.streamDuration = 0
		a.streamDurationID = it.ID
	}
	a.player = p
	a.playing = it
	a.errLogged = ""
	a.current = it
	a.playingQuality = ""
	a.danmaku = nil
	a.playStarted = time.Now()
	a.playReady = false
	a.fresh = fromStart
	a.route = pagePlayer
	a.rate = a.store.Rate()
	a.drawer = false
	a.ended = false
	a.uiHidden = false
	a.uiShown = time.Now()
	a.seekedAt = time.Time{}
	a.seekHold = false
	// The built-in player is told to play below. The display lock is
	// held from here, so the system does not see an idle desktop while
	// the decoder and sound still start.
	a.keepDisplayOn()
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
	if isPhone() {
		a.mobileSetPlayerMode(false)
	}
	// A window left in full screen keeps a style that has no frame to drag or
	// resize by, and the pages after the player have no button to leave it
	// with: it goes back to a window as the player does.
	if a.win != nil && a.win.IsFullScreen() {
		a.win.SetFullScreen(false)
	}
	a.closePlayer()
	a.releaseDisplayLock()
	a.seek = 0
	a.drawer = false
	a.uiHidden = false
	// A live channel has no page of its own to go back to.
	if a.playing.Live || a.playing.Source == "iptv" {
		a.route = pageHome
	} else {
		a.route = pageDetail
	}
	a.invalidate()
}

func (a *app) closePlayer() {
	a.retryPending = ""
	if a.proxyToken != "" && a.proxy != nil {
		a.proxy.Forget(a.proxyToken)
		a.proxyToken = ""
	}
	a.retryStream = streamproxy.Stream{}
	if !a.hasPlayer() {
		return
	}
	a.saveProgress()
	a.stopLiveDanmaku()
	if a.mobile != nil {
		a.mobile.Close()
		a.mobile = nil
	}
	if a.player != nil {
		a.player.Close()
		a.player = nil
	}
	a.avTune = nil
	a.releaseDisplayLock()
}

// saveProgress writes where the video stands, at most what changed.
func (a *app) saveProgress() {
	if !a.hasPlayer() || a.playing.ID == "" {
		return
	}
	// A live channel has no place to keep.
	if a.playing.Source == "iptv" || a.playing.Live {
		return
	}
	// Before the first picture the engine has no position to keep, and a
	// zero there would throw away where the listener actually was.
	if !a.playerReady() {
		return
	}
	pos := a.playhead().Milliseconds()
	if a.seekHold {
		// The seek has not landed yet, and the player may still report the
		// old place: keep the place the hand chose, not a stale zero.
		pos = time.Duration(a.seek * float64(time.Second)).Milliseconds()
	}
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
	if !a.hasPlayer() {
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
	if a.mobile != nil {
		a.mobile.SetVolume(v)
	} else if a.player != nil {
		a.player.SetVolume(v)
	}
}

func (a *app) say(format string, args ...any) {
	a.toast = fmt.Sprintf(format, args...)
	a.toastAt = time.Now()
	a.invalidate()
}

// avTuner is an engine whose picture can be moved against its sound, for an
// output that delays one of them: the GPU one does, the others do not.
type avTuner interface {
	SetAVOffset(time.Duration)
}

// nudgeAV moves the picture against the sound by d, keeps it for the next
// playback, and says where it stands: the audio delay a player offers, which
// is the only way software can know what a screen or a sound card adds.
func (a *app) nudgeAV(d time.Duration) {
	a.avOffset += d
	if a.avOffset > 2*time.Second {
		a.avOffset = 2 * time.Second
	}
	if a.avOffset < -2*time.Second {
		a.avOffset = -2 * time.Second
	}
	if a.avTune != nil {
		a.avTune.SetAVOffset(a.avOffset)
	}
	if a.store != nil {
		a.store.SetAVOffset(a.avOffset)
	}
	side := "画面提前"
	if a.avOffset < 0 {
		side = "画面延后"
	}
	v := a.avOffset
	if v < 0 {
		v = -v
	}
	a.say("%s %d ms", side, v.Milliseconds())
}

// diag writes a line to diag.log, for what a run should leave behind.
// logTools writes what ffmpeg is and can do to the diagnostic log, so a file
// that will not play can be explained from the log alone.
func (a *app) logTools() {
	if a.ffmpeg == "" {
		return
	}
	run := func(args ...string) []byte {
		cmd := exec.Command(a.ffmpeg, append([]string{"-hide_banner"}, args...)...)
		executil.Hide(cmd)
		out, _ := cmd.Output()
		return out
	}
	if out := run("-version"); len(out) > 0 {
		line, _, _ := strings.Cut(string(out), "\n")
		a.diag("ffmpeg: %s", strings.TrimSpace(line))
	}
	if out := run("-encoders"); len(out) > 0 {
		for _, name := range []string{"libx264", "h264_nvenc", "h264_qsv", "h264_amf", "h264_mf"} {
			if strings.Contains(string(out), name) {
				a.diag("encoder: %s", name)
			}
		}
	}
	if out := run("-hwaccels"); len(out) > 0 {
		a.diag("hwaccels: %s", strings.Join(strings.Fields(string(out)), " "))
	}
}

// hevcExtensionInstalled says Microsoft's HEVC decoder is on the machine, in
// which case the player takes HEVC as it is and nothing is transcoded.
func (a *app) hevcExtensionInstalled() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	if a.hevcExtChecked {
		return a.hevcExt
	}
	a.hevcExtChecked = true
	dirs, _ := filepath.Glob(filepath.Join(systemLocalAppData(), "Packages", "Microsoft.HEVCVideoExtension*"))
	a.hevcExt = len(dirs) > 0
	if a.hevcExt {
		a.diag("hevc extension found: %s", dirs[0])
	}
	return a.hevcExt
}

// pickVideoEncoder is the encoder a transcode uses. It is x264 always: GPU
// encoders cannot be trusted, since NVENC takes the frame and crashes on
// Pascal cards with recent drivers, while x264 at 1080p keeps up.
func (a *app) pickVideoEncoder() string {
	return "libx264"
}

// hevcNeedsTranscode says the stream's video is HEVC, which Media Foundation
// cannot decode without Microsoft's extension: its audio would play and its
// picture stay black, without an error to notice.
func (a *app) hevcNeedsTranscode(s streamproxy.Stream) bool {
	if a.ffmpeg == "" || runtime.GOOS != "windows" || a.hevcExtensionInstalled() {
		return false
	}
	// Only a file handed over as it is can be HEVC the player cannot take;
	// HLS, DASH and FLV already go through ffmpeg.
	if s.Type != "" && s.Type != "mp4" && s.Type != "remux" {
		return false
	}
	// The probe opens the stream over the network: one answer is kept for
	// the retries, which would otherwise wait on it again.
	key := s.Type + "\x00" + s.URL + "\x00" + headerArg(s.Headers)
	if hevc, ok := a.hevcProbe[key]; ok {
		return hevc
	}
	args := []string{"-hide_banner", "-rw_timeout", "15000000"}
	args = append(args, ffbin.NoProxyArgs(s.URL)...)
	if h := headerArg(s.Headers); h != "" {
		args = append(args, "-headers", h)
	}
	args = append(args, "-i", s.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.ffmpeg, args...)
	executil.Hide(cmd)
	out, _ := cmd.CombinedOutput()
	hevc := strings.Contains(string(out), "Video: hevc")
	if len(a.hevcProbe) > 512 {
		a.hevcProbe = map[string]bool{}
	}
	a.hevcProbe[key] = hevc
	return hevc
}

func (a *app) diag(format string, args ...any) {
	if a.dataDir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(a.dataDir, "diag.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

// hevcSource says the video's picture is HEVC in the hev1 tag. The probe
// carries the stream's headers, for a WebDAV server that asks for them,
// and its answer is kept for the retries and the next play.
func (a *app) hevcSource(s streamproxy.Stream) bool {
	if a.ffmpeg == "" || s.URL == "" {
		return false
	}
	key := "src\x00" + s.URL + "\x00" + headerArg(s.Headers)
	if hevc, ok := a.hevcProbe[key]; ok {
		return hevc
	}
	args := []string{"-hide_banner", "-rw_timeout", "8000000"}
	args = append(args, ffbin.NoProxyArgs(s.URL)...)
	if h := headerArg(s.Headers); h != "" {
		args = append(args, "-headers", h)
	}
	args = append(args, "-i", s.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.ffmpeg, args...)
	executil.Hide(cmd)
	out, _ := cmd.CombinedOutput()
	hevc := strings.Contains(string(out), "Video: hevc")
	if len(a.hevcProbe) > 512 {
		a.hevcProbe = map[string]bool{}
	}
	a.hevcProbe[key] = hevc
	return hevc
}

// needsFFmpegBeforeOpen says the system's player has no demuxer for the
// file, so ffmpeg has to hand it over as HLS. AVFoundation reads MP4, MOV
// and HLS itself; MKV, AVI, WebM and the rest are left to ffmpeg.
func needsFFmpegBeforeOpen(path string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv", ".avi", ".webm", ".flv", ".wmv", ".rm", ".rmvb",
		".ts", ".m2ts", ".mts", ".vob", ".ogv", ".ogm", ".mpg", ".mpeg",
		".m2v", ".asf", ".divx":
		return true
	}
	return false
}
