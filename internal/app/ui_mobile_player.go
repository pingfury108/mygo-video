package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
	"mygo-video/internal/streamproxy"
	"mygo-video/internal/subtitle"
)

// mobilePlayerPage is the video over the whole screen: the system's decoder
// draws it, and MyGo draws the bars above it.
func (a *app) mobilePlayerPage(c *ui.Context) {
	a.tick()
	if a.mobile == nil {
		a.leavePlayer()
		return
	}
	c.OnShortcut(0, ui.KeyEscape, a.leavePlayer)
	c.OnShortcut(0, ui.KeySpace, a.togglePlay)
	c.OnShortcut(0, ui.KeyLeft, func() { a.seekBy(-10 * time.Second) })
	c.OnShortcut(0, ui.KeyRight, func() { a.seekBy(10 * time.Second) })

	if !a.mobile.Playing() || !a.mobile.Ready() {
		a.uiHidden = false
	} else if !a.uiHidden && time.Since(a.uiShown) > hideAfter {
		a.uiHidden = true
	}
	if a.mobile.Ready() && !a.playReady {
		a.playReady = true
		a.diag("mobile ready after %v", time.Since(a.playStarted))
	}
	if a.mobile.Ready() && a.playbackEnded() {
		if !a.ended {
			a.ended = true
			a.playNext()
		}
	} else {
		a.ended = false
	}

	root := ui.Box(c).Fill().Background(ui.Hex("#000000"))
	if x, y, over := root.PointerPosition(); over {
		if !a.pointerIn || x != a.pointerX || y != a.pointerY {
			a.uiShown = time.Now()
			a.uiHidden = false
		}
		a.pointerX, a.pointerY = x, y
		a.pointerIn = true
	}
	root.Children(func() {
		a.mobile.View(c).Absolute().Top(0).Left(0).Right(0).Bottom(0)
		// A tap on the picture brings the controls back, or puts them away.
		ui.Box(c).Fill().OnClick(func() {
			a.uiShown = time.Now()
			a.uiHidden = !a.uiHidden
			a.invalidate()
		})
		a.mobileSubtitleLayer(c)
		if a.mobile.Ready() && a.mobile.Buffering() {
			a.mobileBuffering(c)
		}
		if a.mobile.Ready() && !a.mobile.Playing() && !a.uiHidden && !a.mobile.Buffering() {
			a.mobileCenterPlay(c)
		}
		a.mobilePlayerTop(c)
		a.mobilePlayerControls(c)
		if err := a.mobile.Err(); err != nil {
			if a.errLogged != a.playing.ID {
				a.errLogged = a.playing.ID
				a.diag("mobile player error item=%s err=%v", a.playing.ID, err)
			}
			a.releaseDisplayLock()
			title, detail := "打不开这个视频", playbackErrorText(err)
			if a.playReady {
				title, detail = "播放中断", "网络或文件读取断开了，可以从当前进度重试。"
			}
			a.playerMessage(c, title, detail, true, a.mobileRetry)
		} else if !a.mobile.Ready() {
			a.playerMessage(c, "正在打开…", "", false, nil)
			c.After(120 * time.Millisecond)
		}
	})
}

func (a *app) mobilePlayerTop(c *ui.Context) {
	shown := float32(1)
	if a.uiHidden {
		shown = 0
	}
	top := ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(58).
		Padding(8, 16, 0, 16).Gap(9).AlignItems(ui.Center).
		Gradient(ui.RGBA(0, 0, 0, 0.76), ui.Transparent, 180).
		Opacity(shown).IgnoreInput(a.uiHidden)
	top.Children(func() {
		playerButton(c, iconBack, 18, "返回", false, a.leavePlayer)
		ui.Column(c).Gap(2).Grow(1).Basis(0).Children(func() {
			title := a.playing.Name
			if ep := a.playing.EpisodeLabel(); ep != "" {
				title += " · " + ep
			}
			ui.Text(c, title).FontSize(12.5).FontWeight(650).SingleLine()
			ui.Text(c, a.playingSourceName()+" · 硬件解码").FontSize(9.5).
				TextColor(ui.RGBA(255, 255, 255, 0.5)).SingleLine()
		})
		ui.Text(c, a.playingSourceName()).FontSize(9.5).TextColor(ui.RGBA(255, 255, 255, 0.72)).
			Padding(3, 8).Radius(20).Background(ui.RGBA(255, 255, 255, 0.11))
	})
}

func (a *app) mobileCenterPlay(c *ui.Context) {
	icon := iconPlay
	ui.Box(c).Fill().Center().OnClick(a.togglePlay).Children(func() {
		ui.Box(c).Size(60, 60).Radius(30).Background(ui.RGBA(10, 11, 13, 0.62)).
			Border(1, ui.RGBA(255, 255, 255, 0.18)).Center().Children(func() {
			ui.Icon(c, icon).Size(24, 24).TextColor(ui.Hex("#ffffff"))
		})
	})
}

func (a *app) mobilePlayerControls(c *ui.Context) {
	shown := float32(1)
	if a.uiHidden {
		shown = 0
	}
	white := ui.RGBA(255, 255, 255, 0.72)
	bar := ui.Column(c).Absolute().Left(0).Right(0).Bottom(0).
		Padding(34, 18, 14, 18).Gap(4).
		Gradient(ui.Transparent, ui.RGBA(0, 0, 0, 0.9), 180).
		Opacity(shown).IgnoreInput(a.uiHidden)
	bar.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, media.HumanDuration(a.playhead().Milliseconds())).
				FontSize(9.5).TextColor(white).Width(44).TextAlign(ui.End)
			a.seekBar(c)
			ui.Text(c, media.HumanDuration(a.playLength().Milliseconds())).
				FontSize(9.5).TextColor(white).Width(44)
		})
		ui.Row(c).Gap(4).AlignItems(ui.Center).Padding(4, 0, 0, 0).Children(func() {
			icon := iconPlay
			if a.mobile.Playing() {
				icon = iconPause
			}
			playerButton(c, icon, 20, "播放/暂停", false, a.togglePlay)
			playerButton(c, iconBack, 15, "后退 10 秒", false, func() { a.seekBy(-10 * time.Second) })
			playerButton(c, iconForward, 15, "快进 10 秒", false, func() { a.seekBy(10 * time.Second) })
			ui.Box(c).Grow(1)
			if len(a.subs) > 0 {
				playerButton(c, iconSubs, 16, "字幕", a.subsOn, a.toggleSubtitles)
			} else if a.playing.Source != "webdav" && a.playing.Path != "" {
				playerButton(c, iconSubs, 16, "添加字幕", false, a.mobilePickSubtitle)
			}
			panscan := ui.ButtonBase(c).Height(32).PaddingX(9).Radius(8).Label("画面比例").
				FocusRing(false).TextColor(ui.RGBA(255, 255, 255, 0.82))
			panscan.OnClick(a.cycleMobilePanscan)
			panscan.Children(func() {
				label := "适应"
				if a.mobilePanscan > 0.5 {
					label = "铺满"
				}
				ui.Text(c, label).FontSize(11).FontWeight(600)
			})
			rateColor := ui.RGBA(255, 255, 255, 0.82)
			if a.rate != 1 {
				rateColor = colors().accent
			}
			rate := ui.ButtonBase(c).Height(32).PaddingX(9).Radius(8).Label("播放速度").
				FocusRing(false).TextColor(rateColor)
			rate.OnClick(a.cycleRate)
			rate.Children(func() { ui.Text(c, rateLabel(a.rate)).FontSize(11).FontWeight(600) })
			if len(a.mobileSeriesOf(a.playing)) > 1 {
				playerButton(c, iconEpisodes, 17, "选集", a.mobileEpisodes, func() {
					a.mobileEpisodes = true
					a.uiShown = time.Now()
					a.uiHidden = false
				})
			}
		})
	})
}

// mobilePickSubtitle adds a subtitle beside a local video: the picker's file
// is copied under the video's name, which is where the player looks for one.
func (a *app) mobilePickSubtitle() {
	video, id := a.playing.Path, a.playing.ID
	if a.playing.Source == "webdav" {
		a.say("WebDAV 视频暂不支持外挂字幕")
		return
	}
	if video == "" {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: a.win,
			Title:  "选择字幕",
			Filters: []mygo.FileFilter{{
				Name:       "字幕",
				MIMETypes:  []string{"text/plain", "application/x-subrip"},
				Extensions: []string{"srt", "vtt", "ass", "ssa"},
			}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		ext := strings.ToLower(filepath.Ext(paths[0]))
		switch ext {
		case ".srt", ".vtt", ".ass", ".ssa":
		default:
			ext = ".srt"
		}
		st, err := os.Stat(paths[0])
		if err != nil {
			mygo.RunOnMain(func() { a.say("读取字幕失败：%v", err) })
			return
		}
		dst := strings.TrimSuffix(video, filepath.Ext(video)) + ext
		if err := copyMobileFile(paths[0], dst, st.Size(), nil); err != nil {
			mygo.RunOnMain(func() { a.say("保存字幕失败：%v", err) })
			return
		}
		mygo.RunOnMain(func() {
			if a.playing.ID != id {
				return
			}
			a.subs = subtitle.Load(video)
			a.subsOn = len(a.subs) > 0
			if a.subsOn {
				a.say("已加载字幕")
			} else {
				a.say("这个字幕文件读不出内容")
			}
			a.invalidate()
		})
	}()
}

// mobileBuffering names the wait a network stall puts on the picture, so a
// still frame does not look like a player that stopped answering.
func (a *app) mobileBuffering(c *ui.Context) {
	ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Center().IgnoreInput(true).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(9, 14).Radius(10).
			Background(ui.RGBA(0, 0, 0, 0.55)).Children(func() {
			ui.Spinner(c).Size(15, 15)
			ui.Text(c, "缓冲中…").FontSize(11.5).TextColor(ui.RGBA(255, 255, 255, 0.85))
		})
	})
}

func (a *app) mobileSubtitleLayer(c *ui.Context) {
	if !a.subsOn || len(a.subs) == 0 || !a.hasPlayer() {
		return
	}
	text := subtitle.At(a.subs, a.playhead())
	if text == "" {
		return
	}
	bottom := float32(104)
	if a.uiHidden {
		bottom = 40
	}
	ui.Box(c).Absolute().Left(12).Right(12).Bottom(bottom).IgnoreInput(true).Children(func() {
		ui.Text(c, text).FontSize(17).FontWeight(600).TextColor(ui.Hex("#ffffff")).
			TextAlign(ui.Center).Padding(3, 10).Radius(5).
			Background(ui.RGBA(0, 0, 0, 0.56))
	})
}

func (a *app) cycleRate() {
	next := rates[0]
	for i, r := range rates {
		if a.rate == r {
			next = rates[(i+1)%len(rates)]
			break
		}
	}
	a.setRate(next)
	a.say("播放速度 %s", rateLabel(next))
}

// cycleMobilePanscan goes between mpv's two useful fits: the whole video,
// letterboxed, and the view filled by cropping its edges.
func (a *app) cycleMobilePanscan() {
	if a.mobilePanscan > 0.5 {
		a.mobilePanscan = 0
		a.say("保持原始比例")
	} else {
		a.mobilePanscan = 1
		a.say("画面铺满，边缘会被裁切")
	}
	if a.mobile != nil {
		a.mobile.SetPanscan(a.mobilePanscan)
	}
}

func (a *app) playMobileLocal(it media.Item, fromStart bool) {
	start := time.Duration(0)
	if !fromStart {
		if prog, ok := a.store.Progress(it.ID); ok && prog.Started() {
			start = time.Duration(prog.Position) * time.Millisecond
		}
	}
	a.playMobileLocalAt(it, start)
}

func (a *app) playMobileLocalAt(it media.Item, start time.Duration) {
	if it.Path == "" {
		a.say("这个视频没有本地文件")
		return
	}
	a.closePlayer()
	a.subs = subtitle.Load(it.Path)
	a.subsOn = len(a.subs) > 0
	a.beginMobilePlayback(it, it.Path, start)
}

func (a *app) playMobileWebDAVAt(it media.Item, start time.Duration) {
	a.closePlayer()
	if a.proxy == nil {
		a.say("本地代理没有启动")
		return
	}
	kind := "mp4"
	if strings.HasSuffix(strings.ToLower(it.RemoteID), ".m3u8") {
		kind = "hls"
	}
	url, token, err := a.proxy.Open(streamproxy.Stream{
		URL:     it.RemoteID,
		Type:    kind,
		Headers: a.webdavAuth(),
	})
	if err != nil {
		a.say("打不开这个视频：%v", err)
		a.invalidate()
		return
	}
	a.proxyToken = token
	a.subs = nil
	a.subsOn = false
	a.beginMobilePlayback(it, url, start)
}

func (a *app) beginMobilePlayback(it media.Item, source string, start time.Duration) {
	if a.dataDirBusy {
		a.say("正在复制数据，暂时不能播放")
		return
	}
	m, err := newMobilePlayback(a.win, source, a.mobilePanscan, func() {
		a.invalidate()
	})
	if err != nil {
		a.say("打不开这个视频：%v", err)
		a.route = pageDetail
		a.invalidate()
		return
	}
	a.mobile = m
	a.player = nil
	a.webdavHLS = false
	a.eventHLS = false
	a.webdavBase = 0
	a.streamDuration = 0
	a.streamDurationID = it.ID
	a.playing = it
	a.current = it
	a.playingQuality = ""
	a.danmaku = nil
	a.playStarted = time.Now()
	a.playReady = false
	a.errLogged = ""
	a.fresh = start <= 0
	a.route = pagePlayer
	a.rate = a.store.Rate()
	a.drawer = false
	a.mobileEpisodes = false
	a.ended = false
	a.uiHidden = false
	a.uiShown = time.Now()
	a.seekedAt = time.Time{}
	a.seekHold = false
	a.keepDisplayOn()
	m.SetRate(a.rate)
	m.SetVolume(a.volume)
	if start > 0 {
		m.Seek(start)
		a.seek = start.Seconds()
		a.seekHold = true
		a.seekedAt = time.Now()
	}
	m.Play()
	a.mobileSetPlayerMode(true)
	a.lastSave = time.Now()
	a.invalidate()
}

func (a *app) mobileRetry() {
	if a.playing.ID == "" {
		return
	}
	resume := a.playhead()
	if a.playing.Source == "webdav" {
		a.playWebDAVAt(a.playing, resume)
		return
	}
	a.playMobileLocalAt(a.playing, resume)
}

func (a *app) mobileSetPlayerMode(on bool) {
	if !isPhone() {
		return
	}
	if on {
		mygo.SetOrientations(mygo.OrientationLandscape)
		mygo.SystemBars.SetStyle(mygo.BarStyleLight)
		mygo.SystemBars.SetHidden(true)
		return
	}
	mygo.SetOrientations(mygo.OrientationAny)
	mygo.SystemBars.SetHidden(false)
	mygo.SystemBars.SetStyle(mygo.BarStyleLight)
}

func (a *app) mobileErrorText(err error) string {
	return fmt.Sprintf("%v", err)
}
