package app

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/executil"
	"mygo-video/internal/local"
	"mygo-video/internal/media"
	"mygo-video/internal/store"
	"mygo-video/internal/streamproxy"
	"mygo-video/internal/webdav"
)

// makeWebDAV reads the server's settings, and the client that lists it.
func (a *app) makeWebDAV() {
	cfg := a.store.WebDAV()
	a.webdavURL, a.webdavUser, a.webdavPass = cfg.URL, cfg.User, cfg.Pass
	a.webdav = nil
	a.webdavPath, a.webdavLoaded = "", ""
	a.webdavHasLoaded = false
	a.webdavEntries, a.webdavVideos = nil, nil
	a.webdavErr = ""
	a.webdavDirs = map[string]webdavDir{}
	a.webdavIndexAt = time.Time{}
	a.webdavAllItems = nil
	if strings.TrimSpace(cfg.URL) == "" {
		return
	}
	c, err := webdav.New(webdav.Config{URL: cfg.URL, User: cfg.User, Pass: cfg.Pass})
	if err != nil {
		a.webdavErr = err.Error()
		return
	}
	a.webdav = c
	a.loadWebDAVCache()
}

// saveWebDAV keeps what the settings page says and lists it again.
func (a *app) saveWebDAV() {
	cfg := store.WebDAVConfig{
		URL:  strings.TrimSpace(a.webdavURL),
		User: strings.TrimSpace(a.webdavUser),
		Pass: a.webdavPass,
	}
	a.store.SetWebDAV(cfg)
	a.makeWebDAV()
	if a.webdavErr != "" {
		a.say("%v", a.webdavErr)
		return
	}
	a.say("WebDAV 已保存")
	if a.source == sourceWebDAV {
		a.openWebDAVPath("")
	}
	a.invalidate()
}

// openWebDAVPath shows a folder of the server.
func (a *app) openWebDAVPath(p string) {
	a.webdavAll = false
	a.webdavPath = p
	a.webdavLoaded = ""
	a.webdavHasLoaded = false
	a.webdavErr = ""
	a.webdavScroll = ui.ScrollState{}
	a.invalidate()
}

// loadWebDAV lists the folder showing, once.
// loadWebDAV lists the folder showing, unless what it said is fresh
// enough: what it said last time shows at once.
func (a *app) loadWebDAV() {
	a.loadWebDAVPath(a.webdavPath, false)
}

func (a *app) loadWebDAVPath(dir string, force bool) {
	if a.webdav == nil {
		return
	}
	if cached, ok := a.webdavDirs[dir]; ok {
		a.applyWebDAVDir(dir, cached.Entries, false)
		if !force && time.Since(time.UnixMilli(cached.At)) < webdavDirTTL {
			return
		}
	}
	if a.webdavBusy {
		return
	}
	a.webdavBusy = true
	a.webdavErr = ""
	go func() {
		entries, err := a.webdav.List(context.Background(), dir)
		mygo.RunOnMain(func() {
			a.webdavBusy = false
			if a.webdavPath != dir {
				return
			}
			if err != nil {
				a.webdavErr = err.Error()
				a.invalidate()
				return
			}
			a.webdavDirs[dir] = webdavDir{At: time.Now().UnixMilli(), Entries: entries}
			a.applyWebDAVDir(dir, entries, true)
			a.saveWebDAVCache()
			a.invalidate()
		})
	}()
}

// webdavItemsFrom are the videos of a folder, as the pages show them.
func (a *app) webdavItemsFrom(entries []webdav.Entry) []media.Item {
	var items []media.Item
	for _, e := range entries {
		if e.Dir {
			continue
		}
		ext := strings.ToLower(path.Ext(e.Path))
		if !local.Extensions[ext] {
			continue
		}
		title, season, episode := local.ParseName(path.Base(e.Path))
		series := ""
		if season != 0 || episode != 0 {
			series = title
		}
		u := a.webdav.URL(e.Path)
		sum := md5.Sum([]byte(u))
		items = append(items, media.Item{
			ID:       "webdav:" + hex.EncodeToString(sum[:])[:12],
			Source:   "webdav",
			RemoteID: u,
			Name:     title,
			Series:   series,
			Season:   season,
			Episode:  episode,
			Ext:      ext,
			Size:     e.Size,
			ModTime:  e.ModTime,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

// playWebDAV plays a file of the server through the proxy: with the
// authorization it asks for, and as HLS remuxed here when the file's moov
// is at its end, which the player streams.
func (a *app) playWebDAV(it media.Item, fromStart bool) {
	start := time.Duration(0)
	if !fromStart {
		if prog, ok := a.store.Progress(it.ID); ok && prog.Started() {
			start = time.Duration(prog.Position) * time.Millisecond
		}
	}
	a.playWebDAVAt(it, start)
}

// playWebDAVAt plays a video of the server from d: a remux that begins
// there, for the files the player can't seek in itself.
func (a *app) playWebDAVAt(it media.Item, d time.Duration) {
	a.closePlayer()
	if a.webdavDurationID != it.ID {
		a.webdavDuration = 0
		a.webdavDurationID = it.ID
	}
	// The GPU decoder plays the file as it is, inside this window: the
	// picture stays on the GPU, and the transcode below is only for the
	// system's player, which has no HEVC decoder.
	var gpu *ui.VideoPlayer
	if !a.webdavGPUFailed[it.ID] {
		gpu, _ = a.gpuPlayer(it)
	}
	if gpu != nil {
		p := gpu
		// The playhead is the player's own, on the file's timeline: a
		// remux of the playback before it must not shift it again.
		a.webdavHLS = false
		a.webdavBase = 0
		a.subs = nil
		a.subsOn = false
		a.retryStream = streamproxy.Stream{URL: it.RemoteID, Headers: a.webdavAuth(), Start: d}
		if d > 0 {
			p.Seek(d)
		}
		a.beginPlayback(it, p, true)
		// A stream whose length its container does not carry: ffmpeg
		// reads the segments and says how long it is.
		a.probeWebDAVDuration(it)
		return
	}
	// mpv plays it in a window of its own, where the sound keeps its clock.
	if a.playMPV(it, d) {
		return
	}
	kind := "mp4"
	if strings.HasSuffix(strings.ToLower(it.RemoteID), ".m3u8") {
		kind = "hls"
	}
	if kind == "mp4" && needsFFmpegBeforeOpen(it.RemoteID) {
		kind = "remux"
	}
	// AVFoundation plays this picture only with the hvcC box the hev1 tag
	// leaves out, which an fMP4 remux writes.
	tagHEVC := false
	if runtime.GOOS == "darwin" && (kind == "mp4" || kind == "remux") &&
		a.hevcSource(streamproxy.Stream{URL: it.RemoteID, Headers: a.webdavAuth()}) {
		kind = "remux"
		tagHEVC = true
	}
	player, token, url, err := a.openStream(streamproxy.Stream{
		URL:     it.RemoteID,
		Type:    kind,
		Headers: a.webdavAuth(),
		FFmpeg:  a.ffmpeg,
		Start:   d,
		TagHEVC: tagHEVC,
	})
	if err != nil {
		a.say("%v", err)
		return
	}
	// Only the remux really starts at d in ffmpeg. A file AVFoundation
	// opens itself starts at zero, and is seeked after opening; counting
	// its playhead from d would put it at the wrong time and trigger the
	// next episode far too early.
	a.webdavHLS = strings.Contains(url, "/hls/")
	a.webdavBase = 0
	if a.webdavHLS {
		a.webdavBase = d
	}
	a.proxyToken = token
	a.subs = nil
	a.subsOn = false
	a.retryStream = streamproxy.Stream{URL: it.RemoteID, Headers: a.webdavAuth(), Start: d}
	a.beginPlayback(it, player, true)
	if !a.webdavHLS && d > 0 {
		player.Seek(d)
		a.seek = d.Seconds()
		a.seekHold = true
		a.seekedAt = time.Now()
	}
	a.probeWebDAVDuration(it)
}

// probeWebDAVDuration asks ffmpeg how long the video is: an HLS remux
// tells the player only what it has made so far.
func (a *app) probeWebDAVDuration(it media.Item) {
	ff := a.ffmpeg
	if ff == "" {
		return
	}
	args := []string{"-hide_banner", "-rw_timeout", "15000000"}
	if h := headerArg(a.webdavAuth()); h != "" {
		args = append(args, "-headers", h)
	}
	args = append(args, "-i", it.RemoteID)
	id := it.ID
	go func() {
		cmd := exec.Command(ff, args...)
		executil.Hide(cmd)
		out, _ := cmd.CombinedOutput()
		sec, ok := ffmpegDuration(string(out))
		if !ok || sec <= 0 {
			return
		}
		mygo.RunOnMain(func() {
			// The listener moved on while ffmpeg read: the length is of
			// the video no longer playing.
			if a.webdavDurationID != id {
				return
			}
			a.webdavDuration = time.Duration(sec * float64(time.Second))
			a.invalidate()
		})
	}()
}

// ffmpegDuration reads the length ffmpeg prints of its input, as
// "Duration: 00:44:56.65".
func ffmpegDuration(out string) (float64, bool) {
	i := strings.Index(out, "Duration: ")
	if i < 0 {
		return 0, false
	}
	s := out[i+len("Duration: "):]
	if len(s) < 11 {
		return 0, false
	}
	var h, m int
	var sec float64
	if _, err := fmt.Sscanf(s[:11], "%d:%d:%f", &h, &m, &sec); err != nil {
		return 0, false
	}
	return float64(h*3600+m*60) + sec, true
}

func headerArg(h map[string]string) string {
	var b strings.Builder
	for k, v := range h {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	return b.String()
}

// webdavHome is the WebDAV source's page: the folders of where it is, and
// the videos there.
func (a *app) webdavHome(c *ui.Context) {
	p := colors()
	if a.webdav == nil {
		ui.Column(c).Grow(1).Children(func() {
			emptyState(c, "还没有配置 WebDAV", "在设置里填服务器地址、用户名和密码，就能浏览服务器上的视频。", func() {
				primaryButton(c, "去设置", iconSettings, func() { a.goPage(pageSettings) })
			})
		})
		return
	}
	if a.webdavAll {
		a.webdavAllView(c)
		return
	}
	a.loadWebDAV()
	if a.webdavErr != "" {
		ui.Column(c).Grow(1).Children(func() {
			emptyState(c, "连不上 WebDAV", a.webdavErr, func() {
				primaryButton(c, "重试", iconRefresh, func() { a.openWebDAVPath(a.webdavPath) })
			})
		})
		return
	}
	if a.webdavBusy && !a.webdavHasLoaded {
		ui.Column(c).Grow(1).Children(func() {
			emptyState(c, "正在载入…", "", nil)
		})
		return
	}

	var folders []webdav.Entry
	for _, e := range a.webdavEntries {
		if e.Dir {
			folders = append(folders, e)
		}
	}

	ui.Scroll(c).Grow(1).TrackScroll(&a.webdavScroll).Children(func() {
		ui.Column(c).Padding(8, 32, 40, 32).Gap(18).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if a.webdavPath != "" {
					iconButton(c, iconBack, 15, "上一级", func() {
						a.openWebDAVPath(parentPath(a.webdavPath))
					})
				}
				name := "/" + a.webdavPath
				if a.webdavPath == "" {
					name = "根目录"
				}
				ui.Text(c, name).FontSize(12).TextColor(p.muted).SingleLine()
				ui.Box(c).Grow(1)
				ghostButton(c, "全部视频", iconSearch, a.showWebDAVAll)
				iconButton(c, iconRefresh, 15, "刷新", func() {
					a.loadWebDAVPath(a.webdavPath, true)
				})
			})

			if len(folders) > 0 {
				ui.Column(c).Gap(2).Children(func() {
					for _, e := range folders {
						a.folderRow(c, e)
					}
				})
			}

			if len(a.webdavVideos) > 0 {
				ui.Column(c).Gap(12).Children(func() {
					sectionHead(c, "视频", fmt.Sprintf("%d 个", len(a.webdavVideos)), nil)
					a.cardRow(c, a.webdavVideos)
				})
			}

			if len(folders) == 0 && len(a.webdavVideos) == 0 {
				emptyState(c, "这个文件夹是空的", "", nil)
			}
		})
	})
}

func (a *app) folderRow(c *ui.Context, e webdav.Entry) {
	p := colors()
	b := ui.ButtonBase(c).Height(38).PaddingX(10).Gap(10).Radius(8).
		AlignItems(ui.Center).Justify(ui.Start).Label(e.Name)
	b.Background(p.panel)
	if b.Hovered() {
		b.Background(p.hover)
	}
	name := e.Path
	b.OnClick(func() { a.openWebDAVPath(name) })
	b.Children(func() {
		ui.Icon(c, iconFolder).Size(15, 15).TextColor(p.faint)
		ui.Text(c, e.Name).FontSize(12.5).TextColor(p.text).SingleLine().Shrink(1)
		ui.Box(c).Grow(1)
		ui.Icon(c, iconForward).Size(13, 13).TextColor(p.faint)
	})
}

func parentPath(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

// showWebDAVAll shows every video kept from the last walk, and walks only
// when there is none to show.
func (a *app) showWebDAVAll() {
	a.webdavAll = true
	if len(a.webdavAllItems) == 0 {
		a.scanWebDAVAll()
	}
	a.invalidate()
}

// ago says how long it has been, as a line reads.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	default:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	}
}

func (a *app) webdavAuth() map[string]string {
	cfg := a.store.WebDAV()
	if cfg.User == "" && cfg.Pass == "" {
		return nil
	}
	return map[string]string{
		"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(cfg.User+":"+cfg.Pass)),
	}
}

func (a *app) webdavItems() []media.Item {
	if a.webdavAll {
		return a.webdavAllItems
	}
	return a.webdavVideos
}

// webdavAllView is every video below the folder showing, as a grid.
func (a *app) webdavAllView(c *ui.Context) {
	p := colors()
	ui.Row(c).Height(48).PaddingX(32).AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Text(c, "全部视频").FontSize(14).FontWeight(600)
		if a.webdavScanning {
			ui.Text(c, fmt.Sprintf("已扫 %d 个文件夹 · %d 个视频", a.webdavScanned, a.webdavFound)).
				FontSize(11).TextColor(p.faint)
			ghostButton(c, "取消", nil, a.cancelWebDAVScan)
		} else {
			ui.Text(c, fmt.Sprintf("%d 个", len(a.webdavAllItems))).FontSize(11).TextColor(p.faint)
			ghostButton(c, "重新扫描", iconRefresh, a.scanWebDAVAll)
			if !a.webdavIndexAt.IsZero() {
				ui.Text(c, "上次扫描 "+ago(a.webdavIndexAt)).FontSize(10.5).TextColor(p.faint)
			}
		}
		ui.Box(c).Grow(1)
		ghostButton(c, "返回文件夹", iconBack, func() {
			a.webdavAll = false
			a.invalidate()
		})
	})
	n := len(a.webdavAllItems)
	if n == 0 {
		if a.webdavScanning {
			emptyState(c, "正在扫描…", "大目录可能要一会儿，可以取消。", nil)
		} else {
			emptyState(c, "没有找到视频", "换一个文件夹再扫，或检查服务器上的文件。", nil)
		}
		return
	}
	ui.Scroll(c).Grow(1).TrackScroll(&a.webdavAllScroll).Children(func() {
		ui.Column(c).Padding(8, 32, 36, 32).Children(func() {
			a.gridRows(c, n, 32, 14, 190, func(i int) {
				a.videoCard(c, a.webdavAllItems[i], 0)
			})
		})
	})
}

// scanWebDAVAll lists every folder below the one showing, keeping the
// videos. It runs off the main thread and can be cancelled.
func (a *app) scanWebDAVAll() {
	if a.webdav == nil || a.webdavScanning {
		return
	}
	a.webdavScanning = true
	a.webdavAll = true
	a.webdavScanned = 0
	a.webdavFound = 0
	gen := atomic.AddInt32(&a.webdavGen, 1)
	a.invalidate()
	go func() {
		type node struct {
			path  string
			depth int
		}
		queue := []node{{path: "", depth: 0}}
		seen := map[string]bool{"": true}
		var items []media.Item
		dirs := 0
		for len(queue) > 0 {
			if atomic.LoadInt32(&a.webdavGen) != gen {
				return
			}
			n := queue[0]
			queue = queue[1:]
			entries, err := a.webdav.List(context.Background(), n.path)
			if err != nil {
				continue
			}
			dirs++
			for _, e := range entries {
				if e.Dir && n.depth < 8 && !seen[e.Path] {
					seen[e.Path] = true
					queue = append(queue, node{path: e.Path, depth: n.depth + 1})
				}
			}
			items = append(items, a.webdavItemsFrom(entries)...)
			if dirs >= 2000 || len(items) >= 20000 {
				break
			}
			if dirs%5 == 0 {
				d, found := dirs, len(items)
				mygo.RunOnMain(func() {
					if atomic.LoadInt32(&a.webdavGen) != gen {
						return
					}
					a.webdavScanned, a.webdavFound = d, found
					a.invalidate()
				})
			}
		}
		mygo.RunOnMain(func() {
			if atomic.LoadInt32(&a.webdavGen) != gen {
				return
			}
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			a.webdavScanning = false
			a.webdavScanned = dirs
			a.webdavAllItems = items
			a.webdavFound = len(items)
			a.webdavIndexAt = time.Now()
			a.saveWebDAVCache()
			a.invalidate()
		})
	}()
}

func (a *app) cancelWebDAVScan() {
	atomic.AddInt32(&a.webdavGen, 1)
	a.webdavScanning = false
	a.webdavAll = false
	a.invalidate()
}
