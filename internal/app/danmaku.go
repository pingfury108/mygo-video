package app

import (
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
	"mygo-video/internal/plugin"
)

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
	prefs := a.danmakuPrefs.WithDefaults()
	life := time.Duration(prefs.Speed * float64(time.Second)).Milliseconds()
	font := float32(prefs.Size)
	rowH := font + 12
	alpha := float32(prefs.Opacity)
	pos := a.playhead().Milliseconds()
	width, height := c.Size()
	rows := int(float64(height) * prefs.Area / float64(rowH))
	rows = max(min(rows, 20), 3)
	ui.Box(c).Fill().IgnoreInput(true).Children(func() {
		start := sort.Search(len(a.danmaku), func(i int) bool {
			return a.danmaku[i].Time > pos-life
		})
		shown := 0
		for i := start; i < len(a.danmaku) && shown < 90; i++ {
			cue := a.danmaku[i]
			age := pos - cue.Time
			if age < 0 || age > life {
				continue
			}
			if blockedWord(cue.Text, prefs.Block) {
				continue
			}
			// 4 stays at the bottom, 5 at the top, for a shorter while;
			// the rest fly across.
			fixed := cue.Mode == 4 || cue.Mode == 5
			if fixed && age > 4000 {
				continue
			}
			// The speed is the time to cross, so a wider picture moves
			// the comments a little faster.
			x := float64(width) - float64(age)*(float64(width)+600)/float64(life)
			// Rows go by what shows now, not by how many comments the video
			// has had: a live chat would otherwise start halfway down.
			top := 56 + float32((shown%rows)*int(rowH))
			if fixed {
				x = float64(width) * 0.34
				if cue.Mode == 4 {
					top = height - (110 + float32((shown%3)*int(rowH)))
				} else {
					top = 56 + float32((shown%3)*int(rowH))
				}
			}
			if x < -500 {
				continue
			}
			shown++
			color := ui.RGBA(255, 255, 255, alpha)
			if cue.Color != "" && cue.Color != "#ffffff" {
				color = ui.Hex(cue.Color)
			}
			ui.Text(c, cue.Text).FontSize(font).TextColor(color).
				Absolute().Left(float32(x)).Top(top).IgnoreInput(true)
		}
	})
}

// blockedWord reports whether a comment holds one of the words to drop.
func blockedWord(text, words string) bool {
	for _, w := range strings.Split(words, ",") {
		w = strings.TrimSpace(w)
		if w != "" && strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// saveDanmaku keeps how the comments look.
func (a *app) saveDanmaku() {
	a.store.SetDanmaku(a.danmakuPrefs)
	a.invalidate()
}

// toggleDanmaku turns the comments on or off.
func (a *app) toggleDanmaku() {
	a.danmakuOn = !a.danmakuOn
	a.uiShown = time.Now()
	a.invalidate()
}

// startLiveDanmaku asks the plugin for the live chat of a channel.
func (a *app) startLiveDanmaku(it media.Item) {
	p := a.pluginByID(it.Source)
	if p == nil || !p.Can("liveDanmaku") {
		return
	}
	go func() { _ = p.LiveDanmaku(it.RemoteID) }()
}

// stopLiveDanmaku closes the live chat the plugin opened.
func (a *app) stopLiveDanmaku() {
	s := a.playing.Source
	if s == "" || s == "webdav" || s == "iptv" {
		return
	}
	p := a.pluginByID(s)
	if p == nil {
		return
	}
	go func() { _ = p.StopLive() }()
}

// liveDanmakuHandler puts what a plugin's live chat pushes at the playhead.
func (a *app) liveDanmakuHandler(p *plugin.Plugin) func(plugin.Cue) {
	return func(cue plugin.Cue) {
		mygo.RunOnMain(func() {
			if a.player == nil || a.playing.Source != p.Info().ID {
				return
			}
			cue.Time = a.playhead().Milliseconds()
			if len(a.danmaku) < 5 {
				a.diag("live cue mode=%d text=%q", cue.Mode, cue.Text)
			}
			// A live chat scrolls; it has no comments fixed to an edge.
			cue.Mode = 0
			a.danmaku = append(a.danmaku, cue)
			if len(a.danmaku) > 300 {
				a.danmaku = a.danmaku[len(a.danmaku)-300:]
			}
			a.invalidate()
		})
	}
}
