package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
)

// view builds the window: a sidebar, the page open, and the player over
// everything.
func (a *app) view(c *ui.Context) {
	a.applyTheme(c)
	c.OnShortcut(ui.Cmd, ui.KeyF, func() {
		a.focusSearch = true
		a.goPage(pageSearch)
	})
	if a.route == pagePlayer && a.player != nil {
		a.playerPage(c)
		return
	}
	if a.route == pagePlayer {
		a.route = pageDetail
	}
	bar := c.TitleBar()
	p := colors()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c, bar)
		ui.Column(c).Grow(1).Background(p.bg).Children(func() {
			a.page(c)
		})
	})
	if a.toast != "" {
		c.Toast(a.toast)
		a.toast = ""
	}
	a.installDialogView(c)
}

func (a *app) page(c *ui.Context) {
	switch a.route {
	case pageLibrary:
		a.libraryPage(c)
	case pageSearch:
		a.searchPage(c)
	case pagePlugins:
		a.pluginsPage(c)
	case pageSignIn:
		a.signInPage(c)
	case pageSettings:
		a.settingsPage(c)
	case pageDetail:
		a.detailPage(c)
	default:
		a.homePage(c)
	}
}

// sidebar is the app's navigation: the pages, what was watched last, and the
// folders it reads.
func (a *app) sidebar(c *ui.Context, bar ui.TitleBar) {
	p := colors()
	top := bar.Bottom + 10
	if top < 44 {
		top = 44
	}
	ui.Column(c).Width(232).Background(p.sidebar).Padding(0, 12, 10, 12).DragWindow().Children(func() {
		ui.Box(c).Height(top)
		// The app's mark, as the Dock shows it, with the pages' arrows.
		ui.Row(c).Gap(9).AlignItems(ui.Center).Padding(0, 9, 10, 9).Children(func() {
			ui.Box(c).Size(20, 20).Radius(6).Background(p.accent).Center().Children(func() {
				ui.Icon(c, iconPlay).Size(9, 9).TextColor(p.onAccent)
			})
			ui.Text(c, "MyGo Video").FontSize(12.5).FontWeight(600).TextColor(p.text).SingleLine()
			ui.Box(c).Grow(1)
			a.stepButton(c, iconBack, "返回", len(a.history) > 0, a.back)
			a.stepButton(c, iconForward, "前进", len(a.forward) > 0, a.goForward)
		})

		// The search field is a way into the page that has the real one.
		sb := ui.ButtonBase(c).Height(30).PaddingX(9).Gap(7).Radius(7).
			Justify(ui.Start).FocusRing(false).Label("搜索")
		if sb.Hovered() {
			sb.Background(ui.RGBA(255, 255, 255, 0.09))
		} else {
			sb.Background(ui.RGBA(255, 255, 255, 0.05))
		}
		sb.OnClick(func() {
			a.focusSearch = true
			a.goPage(pageSearch)
		})
		sb.Children(func() {
			ui.Icon(c, iconSearch).Size(14, 14).TextColor(p.faint)
			ui.Text(c, "搜索影片、剧集").FontSize(12).TextColor(p.faint).SingleLine()
			ui.Box(c).Grow(1)
			ui.Text(c, "⌘F").FontSize(10).TextColor(p.faint).Padding(1, 5).Radius(4).Border(1, p.hairline)
		})

		ui.Box(c).Height(12)
		ui.Column(c).Gap(2).Children(func() {
			a.navItem(c, iconHome, "首页", pageHome, "")
			a.navItem(c, iconLibrary, "片库", pageLibrary, "")
			a.navItem(c, iconSearch, "搜索", pageSearch, "")
		})

		if items := a.recentItems(3); len(items) > 0 {
			ui.Box(c).Height(18)
			ui.Text(c, "最近观看").FontSize(10.5).FontWeight(600).TextColor(p.faint).PaddingX(9).PaddingY(6)
			ui.Column(c).Gap(2).Children(func() {
				for _, it := range items {
					a.recentItem(c, it)
				}
			})
		}

		ui.Box(c).Grow(1)

		ui.Column(c).Gap(2).PaddingY(8).Children(func() {
			badge := ""
			if len(a.plugins) > 0 {
				badge = fmt.Sprintf("%d/%d", a.enabledPlugins(), len(a.plugins))
			}
			a.navItem(c, iconPlugin, "插件", pagePlugins, badge)
			a.navItem(c, iconSettings, "设置", pageSettings, "")
		})
		a.sourceSwitcher(c)
	})
}

func (a *app) navItem(c *ui.Context, icon *ui.SVG, label string, target page, badge string) {
	p := colors()
	active := a.route == target
	b := ui.ButtonBase(c).Height(32).PaddingX(9).Gap(10).Radius(7).AlignItems(ui.Center).
		Justify(ui.Start).FocusRing(false).Label(label)
	switch {
	case active:
		b.Background(p.active)
		b.TextColor(p.text)
	case b.Hovered():
		b.Background(p.hover)
		b.TextColor(p.text)
	default:
		b.TextColor(p.muted)
	}
	b.OnClick(func() { a.goPage(target) })
	b.Children(func() {
		iconColor := p.muted
		if active {
			iconColor = p.accent
		}
		ui.Icon(c, icon).Size(16, 16).TextColor(iconColor)
		ui.Text(c, label).FontSize(13).FontWeight(500).SingleLine()
		if badge != "" {
			ui.Box(c).Grow(1)
			ui.Text(c, badge).FontSize(10).FontWeight(600).TextColor(p.accent).
				Padding(1, 6).Radius(4).Background(ui.RGBA(231, 162, 94, 0.14))
		}
	})
}

// stepButton is one of the sidebar's arrows: a page back, or one forward
// again, dimmed while there is none.
func (a *app) stepButton(c *ui.Context, icon *ui.SVG, label string, enabled bool, onClick func()) {
	p := colors()
	b := ui.ButtonBase(c).Size(26, 26).Center().Radius(6).Label(label).FocusRing(false)
	if enabled {
		b.TextColor(p.muted)
		b.OnClick(onClick)
		if b.Hovered() {
			b.Background(p.hover)
		}
	} else {
		b.TextColor(p.faint)
		b.Disabled(true)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(14, 14)
	})
}

// sourceSwitcher is the foot of the sidebar: which source the pages come
// from, and a way to manage them. It grows into a list of sources as plugins
// arrive.
func (a *app) sourceSwitcher(c *ui.Context) {
	p := colors()
	name := a.sourceName()
	if a.source == sourceLocal && a.lib.Scanning() {
		name = "正在扫描…"
	}
	count := a.sourceCount()
	anchor := ui.ButtonBase(c).Height(32).PaddingX(9).Gap(10).Radius(7).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label("当前源")
	switch {
	case a.sourceMenu:
		anchor.Background(p.active)
		anchor.TextColor(p.text)
	case anchor.Hovered():
		anchor.Background(p.hover)
		anchor.TextColor(p.text)
	default:
		anchor.TextColor(p.muted)
	}
	anchor.OnClick(func() { a.sourceMenu = !a.sourceMenu })
	anchor.Children(func() {
		ui.Icon(c, iconFilm).Size(16, 16).TextColor(p.faint)
		ui.Text(c, name).FontSize(13).FontWeight(500).SingleLine().Shrink(1)
		ui.Box(c).Grow(1)
		ui.Text(c, fmt.Sprintf("%d 个", count)).
			FontSize(11).TextColor(p.faint)
		ui.Icon(c, iconDown).Size(12, 12).TextColor(p.faint)
	})

	ui.PopoverBase(c, anchor, &a.sourceMenu, func(panel ui.Element) {
		panel.MinWidth(220).Margin(6, 0, 6, 0).Padding(6).Gap(2).Radius(10).
			Background(p.panel).Border(1, p.hairline).
			Shadow(0, 10, 28, 2, p.cardShade)

		type choice struct {
			name  string
			index int
			count int
		}
		sources := []choice{
			{name: "本地视频", count: a.lib.Count()},
			{name: "WebDAV", index: sourceWebDAV, count: len(a.webdavItems())},
			{name: "IPTV", index: sourceIPTV, count: len(a.iptvChannels)},
		}
		for i, pe := range a.plugins {
			if pe.p == nil {
				continue
			}
			n := 0
			if a.source == i+sourcePluginBase {
				n = len(a.pluginItems)
			}
			sources = append(sources, choice{name: pe.man.Name, index: i + sourcePluginBase, count: n})
		}
		for _, src := range sources {
			index := src.index
			item := ui.ButtonBase(c).Height(30).PaddingX(8).Gap(8).Radius(7).
				AlignItems(ui.Center).Justify(ui.Start)
			if item.Hovered() {
				item.Background(p.hover)
			}
			item.OnClick(func() { a.switchSource(index) })
			item.Children(func() {
				check := ui.Icon(c, iconCheck).Size(13, 13).TextColor(p.accent)
				if a.source != index {
					check.Opacity(0)
				}
				ui.Text(c, src.name).FontSize(12.5).TextColor(p.text).SingleLine().Shrink(1)
				if src.count > 0 {
					ui.Box(c).Grow(1)
					ui.Text(c, fmt.Sprintf("%d", src.count)).
						FontSize(10.5).TextColor(p.faint)
				}
			})
		}
	})
}

func (a *app) recentItem(c *ui.Context, it media.Item) {
	p := colors()
	b := ui.ButtonBase(c).Height(30).PaddingX(9).Gap(8).Radius(7).AlignItems(ui.Center).
		Justify(ui.Start).FocusRing(false).Label(it.Name)
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.OnClick(func() { a.openDetail(it) })
	b.Children(func() {
		ui.Icon(c, iconFilm).Size(14, 14).TextColor(p.faint)
		ui.Text(c, it.Name).FontSize(12.5).TextColor(p.muted).SingleLine()
	})
}

// recentItems are the videos watched last, most recent first.
func (a *app) recentItems(n int) []media.Item {
	if a.lib == nil {
		return nil
	}
	type watched struct {
		item media.Item
		at   int64
	}
	var list []watched
	for _, it := range a.items() {
		p, ok := a.store.Progress(it.ID)
		if !ok || p.Updated == 0 || !p.Started() {
			continue
		}
		list = append(list, watched{it, p.Updated})
	}
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].at > list[j-1].at; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	var items []media.Item
	for i := 0; i < len(list) && i < n; i++ {
		items = append(items, list[i].item)
	}
	return items
}

func (a *app) sourceLine() string {
	switch a.source {
	case sourceIPTV:
		return "IPTV · " + a.iptvLine()
	case sourceWebDAV:
		if a.webdav == nil {
			return "WebDAV · 未配置"
		}
		if a.webdavBusy {
			return "WebDAV · 正在载入…"
		}
		return fmt.Sprintf("WebDAV · %d 个", len(a.webdavItems()))
	case sourceLocal:
		if a.lib == nil {
			return "本地视频"
		}
		if a.lib.Scanning() {
			return "正在扫描…"
		}
		return fmt.Sprintf("本地视频 · %d 个", a.lib.Count())
	}
	return fmt.Sprintf("%s · %d 个", a.sourceName(), len(a.pluginItems))
}

// sourceCount is how many things the source showing lists.
func (a *app) sourceCount() int {
	switch a.source {
	case sourceLocal:
		return a.lib.Count()
	case sourceWebDAV:
		return len(a.webdavItems())
	case sourceIPTV:
		return len(a.iptvChannels)
	}
	return len(a.pluginItems)
}

func (a *app) libraryLine() string {
	if a.source != sourceLocal {
		return a.sourceLine()
	}
	if a.lib == nil {
		return ""
	}
	if a.lib.Scanning() {
		return "正在扫描文件夹…"
	}
	return fmt.Sprintf("%d 个视频 · %d 个文件夹", a.lib.Count(), len(a.lib.Roots()))
}
