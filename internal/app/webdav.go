package app

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"

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
	if strings.TrimSpace(cfg.URL) == "" {
		return
	}
	c, err := webdav.New(webdav.Config{URL: cfg.URL, User: cfg.User, Pass: cfg.Pass})
	if err != nil {
		a.webdavErr = err.Error()
		return
	}
	a.webdav = c
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
	a.webdavPath = p
	a.webdavLoaded = ""
	a.webdavHasLoaded = false
	a.webdavEntries = nil
	a.webdavVideos = nil
	a.webdavErr = ""
	a.webdavScroll = ui.ScrollState{}
	a.invalidate()
}

// loadWebDAV lists the folder showing, once.
func (a *app) loadWebDAV() {
	if a.webdav == nil || a.webdavBusy || (a.webdavHasLoaded && a.webdavLoaded == a.webdavPath) {
		return
	}
	dir := a.webdavPath
	a.webdavBusy = true
	a.webdavErr = ""
	go func() {
		entries, err := a.webdav.List(context.Background(), dir)
		mygo.RunOnMain(func() {
			a.webdavBusy = false
			if a.webdavPath != dir {
				return
			}
			a.webdavLoaded = dir
			a.webdavHasLoaded = true
			if err != nil {
				a.webdavErr = err.Error()
				a.invalidate()
				return
			}
			a.webdavEntries = entries
			a.webdavVideos = a.webdavItemsFrom(entries)
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

// playWebDAV plays a file of the server through the proxy, with the
// authorization the server asks for.
func (a *app) playWebDAV(it media.Item, fromStart bool) {
	a.closePlayer()
	headers := map[string]string{}
	if cfg := a.store.WebDAV(); cfg.User != "" || cfg.Pass != "" {
		headers["Authorization"] = "Basic " +
			base64.StdEncoding.EncodeToString([]byte(cfg.User+":"+cfg.Pass))
	}
	kind := "mp4"
	if strings.HasSuffix(strings.ToLower(it.RemoteID), ".m3u8") {
		kind = "hls"
	}
	url, token, err := a.proxy.Open(streamproxy.Stream{
		URL:     it.RemoteID,
		Type:    kind,
		Headers: headers,
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
	a.beginPlayback(it, player, fromStart)
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
				iconButton(c, iconRefresh, 15, "刷新", func() {
					a.openWebDAVPath(a.webdavPath)
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
					a.cardRow(c, a.webdavVideos, 216)
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
