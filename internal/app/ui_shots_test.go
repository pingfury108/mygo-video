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
	"mygo-video/internal/store"
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
