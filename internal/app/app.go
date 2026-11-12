package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"

	"mygo-video/internal/local"
	"mygo-video/internal/media"
	"mygo-video/internal/store"
	"mygo-video/internal/subtitle"
	"mygo-video/internal/thumb"
)

const appName = "MyGo Video"

// processStart is when the app's code began, before main: the mark of a dev
// run that says where startup spends its time.
var processStart = time.Now()

type page int

const (
	pageHome page = iota
	pageLibrary
	pageSearch
	pageSettings
	pageDetail
	pagePlayer
)

// app is the state every view reads and changes, on the main thread.
type app struct {
	win   *mygo.Window
	lib   *local.Library
	store *store.Store

	// thumbs are the frames taken of the videos showing, by item, and the
	// ones loading or loaded. Main thread only.
	thumbDir  string
	thumbTool string
	thumbs    map[string]*ui.Bitmap
	thumbBusy map[string]bool
	thumbUsed map[string]time.Time

	route   page
	current media.Item
	query   string
	dataDir string
	// sourceMenu shows the list of sources, from the sidebar's foot.
	sourceMenu bool

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
	a := &app{}
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
	a.thumbTool = thumb.Tool()
	a.thumbs = map[string]*ui.Bitmap{}
	a.thumbBusy = map[string]bool{}
	a.thumbUsed = map[string]time.Time{}
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
	a.route = pageDetail
	a.detailScroll = ui.ScrollState{}
	a.invalidate()
}

// play opens an item, from the start or where the listener stopped.
func (a *app) play(it media.Item, fromStart bool) {
	a.closePlayer()
	p, err := video.Open(it.Path)
	if err != nil {
		a.toast = "打不开这个视频：" + err.Error()
		a.openDetail(it)
		return
	}
	a.player = p
	a.playing = it
	a.fresh = fromStart
	a.route = pagePlayer
	a.subs = subtitle.Load(it.Path)
	a.subsOn = len(a.subs) > 0
	a.rate = 1
	a.drawer = false
	a.uiHidden = false
	a.uiShown = time.Now()
	p.SetRate(1)
	a.seekedAt = time.Time{}
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
	a.saveProgress()
	a.seek = 0
	a.drawer = false
	a.uiHidden = false
	if a.player != nil {
		a.player.Close()
		a.player = nil
	}
	a.route = pageDetail
	a.invalidate()
}

func (a *app) closePlayer() {
	if a.player == nil {
		return
	}
	a.saveProgress()
	a.player.Close()
	a.player = nil
}

// saveProgress writes where the video stands, at most what changed.
func (a *app) saveProgress() {
	p := a.player
	if p == nil || a.playing.ID == "" {
		return
	}
	// Before the first picture the engine has no position to keep, and a
	// zero there would throw away where the listener actually was.
	if !p.Ready() {
		return
	}
	pos := p.Position().Milliseconds()
	dur := p.Duration().Milliseconds()
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
