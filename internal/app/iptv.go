package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"

	"mygo-video/internal/iptv"
	"mygo-video/internal/media"
	"mygo-video/internal/store"
	"mygo-video/internal/streamproxy"
)

// makeIPTV reads the playlist the settings keep.
func (a *app) makeIPTV() {
	a.iptvURL = a.store.IPTV().URL
}

// saveIPTV keeps the playlist address and reads it again.
func (a *app) saveIPTV() {
	a.store.SetIPTV(store.IPTVConfig{URL: strings.TrimSpace(a.iptvURL)})
	a.iptvChannels = nil
	a.iptvGroups = nil
	a.iptvGroup = ""
	a.iptvLoaded = false
	a.iptvErr = ""
	a.loadIPTV()
}

// loadIPTV reads the playlist once, off the main thread.
func (a *app) loadIPTV() {
	if a.iptvLoaded || a.iptvBusy {
		return
	}
	src := strings.TrimSpace(a.iptvURL)
	if src == "" {
		return
	}
	a.iptvBusy = true
	a.invalidate()
	go func() {
		chs, err := iptv.Load(src)
		mygo.RunOnMain(func() {
			a.iptvBusy = false
			a.iptvLoaded = true
			if err != nil {
				a.iptvErr = err.Error()
				a.invalidate()
				return
			}
			a.iptvErr = ""
			a.iptvChannels = chs
			a.iptvGroups = iptvGroups(chs)
			a.invalidate()
		})
	}()
}

// iptvGroups lists the groups of the playlist in the order they appear.
func iptvGroups(chs []iptv.Channel) []string {
	var out []string
	seen := map[string]bool{}
	for _, ch := range chs {
		if ch.Group == "" || seen[ch.Group] {
			continue
		}
		seen[ch.Group] = true
		out = append(out, ch.Group)
	}
	return out
}

// iptvShowing is the channels of the group chosen, or of every group.
func (a *app) iptvShowing() []iptv.Channel {
	if a.iptvGroup == "" {
		return a.iptvChannels
	}
	var out []iptv.Channel
	for _, ch := range a.iptvChannels {
		if ch.Group == a.iptvGroup {
			out = append(out, ch)
		}
	}
	return out
}

func (a *app) iptvLine() string {
	switch {
	case a.iptvURL == "":
		return "未配置"
	case a.iptvBusy:
		return "正在载入…"
	case a.iptvErr != "":
		return "载入失败"
	default:
		return fmt.Sprintf("%d 个频道 · %d 组", len(a.iptvChannels), len(a.iptvGroups))
	}
}

func (a *app) iptvHome(c *ui.Context) {
	a.loadIPTV()

	if a.iptvURL == "" {
		ui.Column(c).Grow(1).Children(func() {
			emptyState(c, "还没有直播列表", "在设置里填一个 M3U/M3U8 地址，或本地播放列表的路径。", func() {
				primaryButton(c, "去设置", iconSettings, func() { a.goPage(pageSettings) })
			})
		})
		return
	}
	if a.iptvErr != "" {
		ui.Column(c).Grow(1).Children(func() {
			emptyState(c, "读不到直播列表", a.iptvErr, func() {
				primaryButton(c, "重试", iconRefresh, func() {
					a.iptvLoaded = false
					a.loadIPTV()
				})
			})
		})
		return
	}
	if len(a.iptvChannels) == 0 {
		ui.Column(c).Grow(1).Children(func() {
			emptyState(c, "正在载入…", "", nil)
		})
		return
	}

	ui.Column(c).Grow(1).Children(func() {
		if len(a.iptvGroups) > 1 {
			ui.ScrollHorizontal(c).Height(36).Children(func() {
				ui.Row(c).PaddingX(32).Gap(6).Children(func() {
					a.iptvChip(c, "全部", "")
					for _, g := range a.iptvGroups {
						a.iptvChip(c, g, g)
					}
				})
			})
		}
		list := a.iptvShowing()
		if len(list) == 0 {
			ui.Column(c).Grow(1).Children(func() {
				emptyState(c, "这一组没有频道", "", nil)
			})
			return
		}
		ui.GridView(c, nil, len(list), 192, 88, func(i int) {
			a.channelCard(c, list[i])
		}).Grow(1).Padding(8, 28, 36, 28)
	})
}

// iptvChip is one group of the playlist.
func (a *app) iptvChip(c *ui.Context, name, group string) {
	p := colors()
	active := a.iptvGroup == group
	b := ui.ButtonBase(c).Height(28).PaddingX(12).Radius(7).Label(name).
		AlignItems(ui.Center).Justify(ui.Center)
	if active {
		b.Background(p.accent).Border(0, p.accent)
	} else {
		b.Background(p.panel)
		if b.Hovered() {
			b.Background(p.hover)
		}
	}
	b.OnClick(func() {
		a.iptvGroup = group
		a.invalidate()
	})
	b.Children(func() {
		color := p.text
		if active {
			color = ui.Hex("#0d0e11")
		}
		ui.Text(c, name).FontSize(11.5).TextColor(color).SingleLine()
	})
}

// channelCard is one live channel: its name and group, played at once.
func (a *app) channelCard(c *ui.Context, ch iptv.Channel) {
	p := colors()
	b := ui.ButtonBase(c).Width(172).Height(72).Radius(9).Padding(10, 12, 10, 12).
		Gap(4).AlignItems(ui.Start).Justify(ui.Center).Label(ch.Name)
	b.Background(p.panel)
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.OnClick(func() { a.playIPTV(ch) })
	b.Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, ch.Name).FontSize(12.5).FontWeight(500).TextColor(p.text).SingleLine()
			ui.Text(c, ch.Group).FontSize(10.5).TextColor(p.faint).SingleLine()
		})
	})
}

// playIPTV plays a channel through the proxy, as a live stream that keeps
// no place.
func (a *app) playIPTV(ch iptv.Channel) {
	a.closePlayer()
	kind := "hls"
	low := strings.ToLower(ch.URL)
	if i := strings.IndexAny(low, "?#"); i >= 0 {
		low = low[:i]
	}
	switch {
	case strings.HasSuffix(low, ".mp4"), strings.HasSuffix(low, ".m4v"), strings.HasSuffix(low, ".mkv"):
		kind = "mp4"
	}
	url, token, err := a.proxy.Open(streamproxy.Stream{
		URL:    ch.URL,
		Type:   kind,
		FFmpeg: a.ffmpeg,
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
	a.beginPlayback(media.Item{
		ID:       "iptv:" + ch.URL,
		Name:     ch.Name,
		Path:     ch.Group,
		Source:   "iptv",
		RemoteID: ch.URL,
	}, player, true)
}
