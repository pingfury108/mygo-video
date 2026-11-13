package app

import (
	"sort"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
)

// danmakuLife is how long a comment flies across the video.
const danmakuLife = 9 * time.Second

// loadDanmaku asks the plugin for the comments of a video.
func (a *app) loadDanmaku(it media.Item) {
	p := a.pluginByID(it.Source)
	if p == nil || !p.Can("danmaku") {
		return
	}
	id := it.ID
	a.danmaku = nil
	a.danmakuBusy = true
	go func() {
		cues, err := p.Danmaku(it.RemoteID)
		mygo.RunOnMain(func() {
			a.danmakuBusy = false
			if a.playing.ID != id {
				return
			}
			if err == nil {
				a.danmaku = cues
			}
			a.invalidate()
		})
	}()
}

// danmakuLayer draws the comments whose time has come, from right to left.
func (a *app) danmakuLayer(c *ui.Context) {
	if !a.danmakuOn || len(a.danmaku) == 0 || a.player == nil {
		return
	}
	pos := a.playhead().Milliseconds()
	width, _ := c.Size()
	ui.Box(c).Fill().IgnoreInput(true).Children(func() {
		start := sort.Search(len(a.danmaku), func(i int) bool {
			return a.danmaku[i].Time > pos-int64(danmakuLife/time.Millisecond)
		})
		shown := 0
		for i := start; i < len(a.danmaku) && shown < 90; i++ {
			cue := a.danmaku[i]
			age := pos - cue.Time
			if age < 0 || age > danmakuLife.Milliseconds() {
				continue
			}
			x := float64(width) - float64(age)*0.11
			if x < -500 {
				continue
			}
			shown++
			top := float32(56 + (i%12)*26)
			color := ui.RGBA(255, 255, 255, 0.92)
			if cue.Color != "" && cue.Color != "#ffffff" {
				color = ui.Hex(cue.Color)
			}
			ui.Text(c, cue.Text).FontSize(14).TextColor(color).
				Absolute().Left(float32(x)).Top(top).IgnoreInput(true)
		}
	})
}

// toggleDanmaku turns the comments on or off.
func (a *app) toggleDanmaku() {
	a.danmakuOn = !a.danmakuOn
	a.uiShown = time.Now()
	a.invalidate()
}
