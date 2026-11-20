package app

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
	"mygo-video/internal/subtitle"
)

// hideAfter is how long the controls stay once the pointer stops moving, as
// a video keeps playing.
const hideAfter = 3 * time.Second

var rates = []float64{0.5, 0.75, 1, 1.25, 1.5, 2}

// playerPage shows the video, over everything, with its controls on top.
func (a *app) playerPage(c *ui.Context) {
	a.tick()
	pl := a.player

	c.OnShortcut(0, ui.KeyEscape, a.leavePlayer)
	c.OnShortcut(0, ui.KeySpace, a.togglePlay)
	c.OnShortcut(0, ui.KeyLeft, func() { a.seekBy(-10 * time.Second) })
	c.OnShortcut(0, ui.KeyRight, func() { a.seekBy(10 * time.Second) })
	c.OnShortcut(0, ui.KeyUp, func() { a.setVolume(a.volume + 0.05) })
	c.OnShortcut(0, ui.KeyDown, func() { a.setVolume(a.volume - 0.05) })
	c.OnShortcut(0, ui.KeyC, a.toggleSubtitles)
	c.OnShortcut(0, ui.KeyF, a.toggleFullScreen)
	c.OnShortcut(0, ui.KeyComma, func() { a.playNeighbor(-1) })
	c.OnShortcut(0, ui.KeyPeriod, func() { a.playNeighbor(1) })

	if a.drawer || !pl.Playing() || !pl.Ready() {
		a.uiHidden = false
	} else if !a.uiHidden && time.Since(a.uiShown) > hideAfter {
		a.uiHidden = true
	}
	// A series goes on to its next episode when one ends.
	if pl.Ready() && !a.playReady {
		a.playReady = true
		a.diag("ready after %v", time.Since(a.playStarted))
	}
	// A series goes on to its next episode when one ends. The player reports
	// Ended late, or never, so the end is read off the clock as well.
	if pl.Ready() && (pl.Ended() || a.atEnd()) {
		if !a.ended {
			a.ended = true
			a.playNext()
		}
	} else {
		a.ended = false
	}

	root := ui.Box(c).Fill().Background(ui.Hex("#000000"))
	// Any move brings the controls back; at the edges of the window they
	// stay, where the pointer goes to leave it or to find them.
	if x, y, over := root.PointerPosition(); over {
		if !a.pointerIn || x != a.pointerX || y != a.pointerY {
			a.uiShown = time.Now()
			a.uiHidden = false
		}
		a.pointerX, a.pointerY = x, y
		a.pointerIn = true
		if _, h := c.Size(); y < 90 || y > h-130 {
			a.uiShown = time.Now()
			a.uiHidden = false
		}
	}
	root.Children(func() {
		// A click on the picture pauses it; the bars over it keep their own
		// clicks, and a press there does not reach the picture.
		ui.Video(c, pl).Fill().Fit(ui.Contain).OnClick(a.togglePlay)
		a.subtitleLayer(c)
		a.danmakuLayer(c)
		a.playerTop(c)
		a.playerControls(c)
		a.episodeDrawer(c)
		if err := pl.Err(); err != nil {
			if a.errLogged != a.playing.ID {
				a.errLogged = a.playing.ID
				a.diag("player error item=%s err=%v", a.playing.ID, err)
			}
			if a.canRetry() {
				msg := "正在换一种方式打开…"
				if a.retryStage[a.playing.ID] >= 2 {
					msg = "正在转码，可能较慢…"
				}
				time.AfterFunc(80*time.Millisecond, func() {
					mygo.RunOnMain(a.retryPlayback)
				})
				a.playerMessage(c, msg, "", false)
			} else {
				a.playerMessage(c, "打不开这个视频", err.Error(), true)
			}
		} else if !pl.Ready() {
			a.playerMessage(c, "正在打开…", "", false)
			c.After(120 * time.Millisecond)
		}
	})
}

func (a *app) playerTop(c *ui.Context) {
	shown := float32(1)
	if a.uiHidden {
		shown = 0
	}
	// Under the window's controls, as a sidebar starts: the title bar of a
	// video draws them over the picture, and nothing of ours goes in it.
	padTop := c.TitleBar().Bottom + 8
	// The page fills the window, the title bar included: a press on this bar
	// drags the window, rather than pause the picture below it.
	top := ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(padTop+56).
		Padding(padTop, 20, 0, 20).Gap(12).DragWindow().
		AlignItems(ui.Center).Gradient(ui.RGBA(0, 0, 0, 0.72), ui.Transparent, 180).
		Opacity(shown).IgnoreInput(a.uiHidden)
	top.Children(func() {
		playerButton(c, iconBack, 18, "返回", false, a.leavePlayer)
		ui.Column(c).Gap(3).Children(func() {
			title := a.playing.Name
			if ep := a.playing.EpisodeLabel(); ep != "" {
				title += " · " + ep
			}
			ui.Text(c, title).FontSize(14).FontWeight(600).SingleLine()
			ui.Text(c, a.playing.Subtitle()).FontSize(11).
				TextColor(ui.RGBA(255, 255, 255, 0.5)).SingleLine()
		})
		ui.Box(c).Grow(1)
		ui.Text(c, a.playingSourceName()).FontSize(10.5).TextColor(ui.RGBA(255, 255, 255, 0.66)).
			Padding(3, 9).Radius(20).Background(ui.RGBA(255, 255, 255, 0.1))
	})
}

func (a *app) playerControls(c *ui.Context) {
	pl := a.player
	if pl.Ended() {
		a.saveProgress()
	}
	shown := float32(1)
	if a.uiHidden {
		shown = 0
	}
	white := ui.RGBA(255, 255, 255, 0.72)
	bar := ui.Column(c).Absolute().Left(0).Right(0).Bottom(0).Padding(18, 24, 16, 24).Gap(6).
		Gradient(ui.Transparent, ui.RGBA(0, 0, 0, 0.88), 180).
		Opacity(shown).IgnoreInput(a.uiHidden)
	bar.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			icon := iconPlay
			if pl.Playing() {
				icon = iconPause
			}
			playerButton(c, icon, 20, "播放/暂停", false, a.togglePlay)

			eps := a.seriesOf(a.playing)
			if len(eps) > 0 {
				if _, ok := a.neighbor(a.playing, -1); ok {
					playerButton(c, iconPrev, 16, "上一集", false, func() { a.playNeighbor(-1) })
				}
				if _, ok := a.neighbor(a.playing, 1); ok {
					playerButton(c, iconNext, 16, "下一集", false, func() { a.playNeighbor(1) })
				}
			}

			playerButton(c, iconBack, 15, "后退 10 秒", false, func() { a.seekBy(-10 * time.Second) })
			playerButton(c, iconForward, 15, "快进 10 秒", false, func() { a.seekBy(10 * time.Second) })

			// The playhead sits between the steps and the sound: the bar that
			// seeks, with how far it stands at either end.
			ui.Text(c, media.HumanDuration(a.playhead().Milliseconds())).
				FontSize(11).TextColor(white).Width(46).TextAlign(ui.End)
			a.seekBar(c)
			ui.Text(c, media.HumanDuration(a.playLength().Milliseconds())).
				FontSize(11).TextColor(white).Width(46)

			ui.Box(c).Width(2)
			volumeIcon := iconVolume
			if a.volume <= 0.001 {
				volumeIcon = iconMute
			}
			playerButton(c, volumeIcon, 16, "静音", false, func() {
				if a.volume > 0.001 {
					a.setVolume(0)
				} else {
					a.setVolume(0.8)
				}
				a.invalidate()
			})
			if v := ui.Slider(c, &a.volume, 0, 1).Width(92).Tooltip("音量"); v.Changed() {
				a.setVolume(a.volume)
			}

			if len(a.subs) > 0 {
				playerButton(c, iconSubs, 17, "字幕", a.subsOn, a.toggleSubtitles)
			}
			if len(a.danmaku) > 0 || a.danmakuBusy {
				dm := ui.ButtonBase(c).Height(30).PaddingX(8).Radius(7).Label("弹幕").FocusRing(false).Tooltip("弹幕开关")
				dmColor := ui.RGBA(255, 255, 255, 0.82)
				if a.danmakuOn {
					dmColor = colors().accent
				}
				dm.TextColor(dmColor)
				if dm.Hovered() {
					dm.Background(ui.RGBA(255, 255, 255, 0.12))
				}
				dm.OnClick(a.toggleDanmaku)
				dm.Children(func() {
					ui.Text(c, "弹").FontSize(11).FontWeight(600)
				})
			}
			// The rate opens a list: slow or fast, where the viewer picks one.
			rateColor := ui.RGBA(255, 255, 255, 0.82)
			if a.rate != 1 {
				rateColor = colors().accent
			}
			ui.MenuButton(c, rateLabel(a.rate), func(m *ui.Menu) {
				for _, r := range rates {
					if m.Item(rateLabel(r)).Checked(a.rate == r).Chosen() {
						a.setRate(r)
					}
				}
			}).Height(30).TextColor(rateColor).Tooltip("播放速度")
			if a.pluginQualitySource() {
				q := ui.ButtonBase(c).Height(30).PaddingX(8).Radius(7).Label("清晰度").FocusRing(false).Tooltip("清晰度")
				q.TextColor(ui.RGBA(255, 255, 255, 0.82))
				if q.Hovered() {
					q.Background(ui.RGBA(255, 255, 255, 0.12))
				}
				q.OnClick(a.cycleQuality)
				q.Children(func() {
					ui.Text(c, a.qualityLabel()).FontSize(11).FontWeight(600)
				})
			}
			if len(eps) > 0 {
				playerButton(c, iconEpisodes, 17, "选集", a.drawer, func() {
					a.drawer = !a.drawer
					a.uiHidden = false
				})
			}
			playerButton(c, iconFull, 17, "全屏", false, a.toggleFullScreen)

			if pl.Ended() {
				ui.Text(c, "已看完").FontSize(11.5).TextColor(white).PaddingX(8)
			}
		})
	})
}

// playerButton is a control over the video, drawn light on black.
func playerButton(c *ui.Context, icon *ui.SVG, size float32, label string, active bool, onClick func()) ui.Element {
	p := colors()
	b := ui.ButtonBase(c).Size(size+18, size+18).Center().Radius(8).Label(label).FocusRing(false).Tooltip(label)
	color := ui.RGBA(255, 255, 255, 0.82)
	if active {
		color = p.accent
	}
	b.TextColor(color)
	if b.Hovered() {
		b.Background(ui.RGBA(255, 255, 255, 0.12))
	}
	b.OnClick(onClick)
	b.Children(func() {
		ui.Icon(c, icon).Size(size, size)
	})
	return b
}

// playhead is where the video plays, from the start of the file: a WebDAV
// remux starts where it was asked to.
func (a *app) playhead() time.Duration {
	if a.player == nil {
		return 0
	}
	return a.webdavBase + a.player.Position()
}

// playLength is how long the video is, which ffmpeg tells for a remux.
func (a *app) playLength() time.Duration {
	if a.webdavDuration > 0 {
		return a.webdavDuration
	}
	if a.player != nil {
		return a.player.Duration()
	}
	return 0
}

// atEnd says the playhead reached the end of the video. The player's Ended
// comes late, or never, as its engine reads the state once, when the first
// picture shows, so the end is read off the clock instead. Half a second
// short of it: further back a seek lands, whose landing would count as an
// end, and further back the watched mark of a video cut early misses.
func (a *app) atEnd() bool {
	length := a.playLength()
	if length <= 0 {
		return false
	}
	return a.playhead() >= length-500*time.Millisecond
}

// seekTo moves to d, the whole file's time: a remux starts over there.
func (a *app) seekTo(d time.Duration) {
	pl := a.player
	if pl == nil {
		return
	}
	if d < 0 {
		d = 0
	}
	if length := a.playLength(); length > 0 && d > length-500*time.Millisecond {
		d = length - time.Second
		if d < 0 {
			d = 0
		}
	}
	if a.webdavHLS {
		a.seekWebDAV(d)
		return
	}
	a.seekHold = true
	pl.Seek(d)
	a.seek = d.Seconds()
	a.seekedAt = time.Now()
	a.uiShown = time.Now()
	a.invalidate()
}

func (a *app) seekWebDAV(d time.Duration) {
	if a.playing.ID == "" {
		return
	}
	a.playWebDAVAt(a.playing, d)
}

func (a *app) seekBar(c *ui.Context) {
	dur := a.playLength().Seconds()
	if dur <= 0 {
		dur = 1
	}
	// The knob stays where the hand put it until the video answers the seek:
	// the playhead still tells the old place while a remux restarts, and
	// following it snaps the knob back and forth.
	if a.seekHold && (math.Abs(a.playhead().Seconds()-a.seek) < 1.2 || time.Since(a.seekedAt) > 15*time.Second) {
		a.seekHold = false
	}
	// The knob follows the playhead only when nothing holds it: pulling it
	// back mid-drag is what made it jump to and fro.
	if !a.seekHold && !a.seekDragging && time.Since(a.seekedAt) > 800*time.Millisecond {
		a.seek = a.playhead().Seconds()
	}
	slider := ui.Slider(c, &a.seek, 0, dur).Grow(1)
	// Under the pointer the bar names the time it stands for: a click takes
	// the video straight there.
	x, _, over := slider.PointerPosition()
	b := slider.Bounds()
	if over && b.W > 0 {
		ms := int64(float64(x/b.W) * dur * float64(time.Millisecond))
		slider = slider.Tooltip(media.HumanDuration(ms))
	} else {
		slider = slider.Tooltip("进度")
	}
	dragging := slider.Dragging()
	switch {
	case dragging:
		// The hand holds the knob: the seek waits for the let-go, and the
		// knob holds the hand's place meanwhile.
		a.seekDrag = true
		a.seekedAt = time.Now()
		a.uiShown = time.Now()
	case slider.Changed() && !a.seekDrag && time.Since(a.seekedAt) > 400*time.Millisecond:
		// A press that never became a drag reports its value again each
		// frame the hand trembles: seek on the first of them alone.
		a.seekHold = true
		a.seekedAt = time.Now()
		a.uiShown = time.Now()
		a.seekTo(time.Duration(a.seek * float64(time.Second)))
	case a.seekDrag:
		// The hand let a drag go: the video goes once, where it stopped.
		a.seekDrag = false
		a.seekHold = true
		a.seekedAt = time.Now()
		a.uiShown = time.Now()
		a.seekTo(time.Duration(a.seek * float64(time.Second)))
	}
	a.seekDragging = dragging
}

// subtitleLayer shows the cue under the playhead.
func (a *app) subtitleLayer(c *ui.Context) {
	if !a.subsOn || len(a.subs) == 0 || a.player == nil {
		return
	}
	text := subtitle.At(a.subs, a.player.Position())
	if text == "" {
		return
	}
	// Out of the controls' way, and down on the picture once they go.
	bottom := float32(112)
	if a.uiHidden {
		bottom = 44
	}
	ui.Box(c).Absolute().Left(0).Right(0).Bottom(bottom).Center().Children(func() {
		ui.Text(c, text).FontSize(19).FontWeight(600).TextColor(ui.Hex("#ffffff")).
			TextAlign(ui.Center).MaxWidth(920).Padding(4, 12).Radius(5).
			Background(ui.RGBA(0, 0, 0, 0.55))
	})
}

// episodeDrawer lists the episodes of what plays, down the right side.
func (a *app) episodeDrawer(c *ui.Context) {
	if !a.drawer {
		return
	}
	eps := a.seriesOf(a.playing)
	if len(eps) == 0 {
		a.drawer = false
		return
	}
	p := colors()
	ui.Column(c).Absolute().Top(0).Right(0).Bottom(0).Width(324).
		Background(ui.RGBA(11, 13, 17, 0.97)).Border(1, p.hairline).Children(func() {
		ui.Row(c).Padding(20, 16, 12, 16).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "选集").FontSize(14).FontWeight(600)
			ui.Text(c, fmt.Sprintf("%d 集", len(eps))).FontSize(11).TextColor(p.faint)
			ui.Box(c).Grow(1)
			iconButton(c, iconClose, 14, "关闭", func() { a.drawer = false })
		})
		ui.Scroll(c).Grow(1).Padding(8, 12, 24, 12).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				for _, ep := range eps {
					a.drawerEpisode(c, ep)
				}
			})
		})
	})
}

func (a *app) drawerEpisode(c *ui.Context, ep media.Item) {
	p := colors()
	current := ep.ID == a.playing.ID
	b := ui.ButtonBase(c).Height(40).PaddingX(10).Gap(10).Radius(7).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(ep.EpisodeLabel()).Tooltip(ep.Name)
	switch {
	case current:
		b.Background(ui.RGBA(231, 162, 94, 0.14))
	case b.Hovered():
		b.Background(p.hover)
	}
	b.OnClick(func() {
		a.drawer = false
		a.play(ep, false)
	})
	b.Children(func() {
		color := p.muted
		if current {
			color = p.accent
		}
		ui.Text(c, ep.EpisodeLabel()).FontSize(11).FontWeight(700).Width(56).TextColor(color)
		ui.Text(c, fmt.Sprintf("第 %d 集", ep.Episode)).FontSize(12).
			TextColor(color).SingleLine()
	})
}

func (a *app) playerMessage(c *ui.Context, title, detail string, failure bool) {
	ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Center().Children(func() {
		ui.Column(c).Gap(12).AlignItems(ui.Center).Children(func() {
			if !failure {
				ui.Spinner(c).Size(22, 22)
			}
			ui.Text(c, title).FontSize(15).FontWeight(600)
			if detail != "" {
				ui.Text(c, detail).FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.6)).
					MaxWidth(520).TextAlign(ui.Center)
			}
			if failure {
				ghostButton(c, "返回", iconBack, a.leavePlayer)
			}
		})
	})
}

func (a *app) togglePlay() {
	pl := a.player
	if pl == nil {
		return
	}
	switch {
	case pl.Playing():
		pl.Pause()
		a.saveProgress()
	case pl.Ended():
		if a.webdavHLS {
			a.seekWebDAV(0)
			return
		}
		pl.Seek(0)
		pl.Play()
		a.seek = 0
		a.seekHold = true
		a.seekedAt = time.Now()
	default:
		pl.Play()
	}
	a.uiShown = time.Now()
	a.uiHidden = false
	a.invalidate()
}

func (a *app) seekBy(d time.Duration) {
	if a.player == nil {
		return
	}
	a.seekTo(a.playhead() + d)
}

func (a *app) toggleSubtitles() {
	if len(a.subs) == 0 {
		return
	}
	a.subsOn = !a.subsOn
	a.uiShown = time.Now()
	a.invalidate()
}

// pluginQualitySource says the video comes from a plugin, which may pick
// its quality.
func (a *app) pluginQualitySource() bool {
	s := a.playing.Source
	return s != "" && s != "webdav" && s != "iptv"
}

// qualityLabel is what the stream plays at, or what it will ask for.
func (a *app) qualityLabel() string {
	if a.playingQuality != "" {
		return a.playingQuality
	}
	if a.quality != "" {
		return a.quality + "P"
	}
	return "自动"
}

// cycleQuality asks for the next quality, and plays the video again.
func (a *app) cycleQuality() {
	switch a.quality {
	case "":
		a.quality = "1080"
	case "1080":
		a.quality = "720"
	default:
		a.quality = ""
	}
	if a.pluginQualitySource() {
		a.play(a.playing, false)
	}
	a.uiShown = time.Now()
	a.invalidate()
}

func (a *app) toggleFullScreen() {
	if a.win != nil {
		a.win.ToggleFullScreen()
	}
}

func rateLabel(r float64) string {
	return strconv.FormatFloat(r, 'f', -1, 64) + "×"
}

// playingSourceName is where the video playing came from.
func (a *app) playingSourceName() string {
	switch a.playing.Source {
	case "":
		return "本地"
	case "webdav":
		return "WebDAV"
	case "iptv":
		return "IPTV"
	}
	if pe := a.pluginEntryByID(a.playing.Source); pe != nil {
		return pe.man.Name
	}
	return a.playing.Source
}

// setRate plays the video at r, keeping it for the next one.
func (a *app) setRate(r float64) {
	a.rate = r
	a.store.SetRate(r)
	if a.player != nil {
		a.player.SetRate(r)
	}
	a.uiShown = time.Now()
	a.invalidate()
}
