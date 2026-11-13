package app

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
	"mygo-video/internal/subtitle"
)

// hideAfter is how long the controls stay once the pointer stops moving, as
// a video keeps playing.
const hideAfter = 3 * time.Second

var rates = []float64{1, 1.25, 1.5, 2}

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
	root.OnClick(a.togglePlay)
	root.Children(func() {
		ui.Video(c, pl).Fill().Fit(ui.Contain)
		a.subtitleLayer(c)
		a.playerTop(c)
		a.playerControls(c)
		a.episodeDrawer(c)
		if err := pl.Err(); err != nil {
			a.playerMessage(c, "打不开这个视频", err.Error(), true)
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
	top := ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(padTop+56).
		Padding(padTop, 20, 0, 20).Gap(12).
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
		ui.Text(c, "本地").FontSize(10.5).TextColor(ui.RGBA(255, 255, 255, 0.66)).
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
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, media.HumanDuration(a.playhead().Milliseconds())).
				FontSize(11).TextColor(white).Width(54).TextAlign(ui.End)
			a.seekBar(c)
			ui.Text(c, media.HumanDuration(a.playLength().Milliseconds())).
				FontSize(11).TextColor(white).Width(54)
		})
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
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

			ui.Box(c).Width(6)
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
			if v := ui.Slider(c, &a.volume, 0, 1).Width(92); v.Changed() {
				a.setVolume(a.volume)
			}

			ui.Box(c).Grow(1)

			if len(a.subs) > 0 {
				playerButton(c, iconSubs, 17, "字幕", a.subsOn, a.toggleSubtitles)
			}
			rate := ui.ButtonBase(c).Height(30).PaddingX(8).Radius(7).Label("倍速").FocusRing(false)
			rateColor := ui.RGBA(255, 255, 255, 0.82)
			if a.rate != 1 {
				rateColor = colors().accent
			}
			rate.TextColor(rateColor)
			if rate.Hovered() {
				rate.Background(ui.RGBA(255, 255, 255, 0.12))
			}
			rate.OnClick(a.cycleRate)
			rate.Children(func() {
				ui.Text(c, rateLabel(a.rate)).FontSize(11).FontWeight(600)
			})
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
	b := ui.ButtonBase(c).Size(size+18, size+18).Center().Radius(8).Label(label).FocusRing(false)
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

// playLength is how long the video is, which ffprobe tells for a remux.
func (a *app) playLength() time.Duration {
	if a.webdavDuration > 0 {
		return a.webdavDuration
	}
	if a.player != nil {
		return a.player.Duration()
	}
	return 0
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
	if time.Since(a.seekedAt) > 800*time.Millisecond {
		a.seek = a.playhead().Seconds()
	}
	slider := ui.Slider(c, &a.seek, 0, dur).Grow(1)
	if slider.Changed() {
		a.seekedAt = time.Now()
		a.seekTo(time.Duration(a.seek * float64(time.Second)))
		a.uiShown = time.Now()
	}
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
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(ep.EpisodeLabel())
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

func (a *app) cycleRate() {
	next := rates[0]
	for i, r := range rates {
		if a.rate == r {
			next = rates[(i+1)%len(rates)]
			break
		}
	}
	a.rate = next
	a.player.SetRate(next)
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
