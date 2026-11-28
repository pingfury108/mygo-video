package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"

	"mygo-video/internal/media"
)

// The phone's shell: four tabs, a detail page, and a player over all of it.
// The pages read the same library, store and WebDAV client as the desktop;
// only how they are drawn and how a video is opened differ.

func (a *app) mobileView(c *ui.Context) {
	if a.route == pagePlayer {
		if a.mobile == nil {
			a.route = a.mobileTabPage()
		} else {
			a.mobilePlayerPage(c)
			a.mobileSheet(c)
			a.mobileToast(c)
			return
		}
	}
	a.mobileNormalizeRoute()
	if a.mobileCanBack() {
		c.OnShortcut(0, ui.KeyEscape, a.mobileBack)
	}
	ui.Box(c).Fill().Background(colors().bg).Children(func() {
		ui.Column(c).Fill().Children(func() {
			a.mobilePage(c).Grow(1)
			if a.route != pageDetail {
				a.mobileTabBar(c)
			}
		})
	})
	a.mobileSheet(c)
	a.mobileToast(c)
}

func (a *app) mobileNormalizeRoute() {
	switch a.route {
	case pageHome, pageLibrary, pageSearch, pageSettings, pageDetail:
		return
	default:
		a.route = a.mobileTabPage()
	}
}

func (a *app) mobileTabPage() page {
	switch a.mobileTab {
	case 1:
		return pageLibrary
	case 2:
		return pageSearch
	case 3:
		return pageSettings
	default:
		return pageHome
	}
}

func (a *app) mobileSwitchTab(i int) {
	a.mobileTab = i
	a.route = a.mobileTabPage()
	a.history = nil
	a.forward = nil
	a.uiShown = time.Now()
	a.uiHidden = false
	a.invalidate()
}

func (a *app) mobileCanBack() bool {
	return a.mobileWebdavSheet || a.mobileEpisodes || a.route == pagePlayer ||
		a.route == pageDetail || a.route != a.mobileTabPage() || a.mobileTab != 0
}

func (a *app) mobileBack() {
	switch {
	case a.mobileEpisodes:
		a.mobileEpisodes = false
	case a.mobileWebdavSheet:
		a.mobileWebdavSheet = false
	case a.route == pagePlayer:
		a.leavePlayer()
		return
	case a.route == pageDetail:
		if len(a.history) > 0 {
			a.back()
			return
		}
		a.route = a.mobileTabPage()
	case strings.TrimSpace(a.query) != "":
		a.query = ""
	case a.mobileTab != 0:
		a.mobileSwitchTab(0)
		return
	}
	a.invalidate()
}

func (a *app) mobilePage(c *ui.Context) ui.Element {
	switch a.route {
	case pageLibrary:
		return a.mobileLibraryPage(c)
	case pageSearch:
		return a.mobileSearchPage(c)
	case pageSettings:
		return a.mobileMinePage(c)
	case pageDetail:
		return a.mobileDetailPage(c)
	default:
		return a.mobileHomePage(c)
	}
}

func (a *app) mobileTabBar(c *ui.Context) ui.Element {
	p := colors()
	bar := ui.Row(c).FillWidth().Height(62).Padding(5, 8, 7, 8).Gap(2).
		Background(p.sidebar).Border(1, p.hairline)
	tabs := []struct {
		label string
		icon  *ui.SVG
	}{
		{"首页", iconHome},
		{"片库", iconLibrary},
		{"搜索", iconSearch},
		{"我的", iconUser},
	}
	bar.Children(func() {
		for i, tab := range tabs {
			on := i == a.mobileTab && a.route != pageDetail
			color := p.faint
			if on {
				color = p.accent
			}
			b := ui.ButtonBase(c.Key(i)).Grow(1).Height(50).Radius(12).
				Column().Center().Gap(3).FocusRing(false).Label(tab.label)
			if on {
				b.Background(ui.RGBA(231, 162, 94, 0.10))
			}
			b.OnClick(func() { a.mobileSwitchTab(i) })
			b.Children(func() {
				ui.Icon(c, tab.icon).Size(20, 20).TextColor(color)
				ui.Text(c, tab.label).FontSize(9.5).TextColor(color)
			})
		}
	})
	return bar
}

func (a *app) mobileToast(c *ui.Context) {
	if a.toast == "" {
		return
	}
	if a.toastAt.IsZero() || time.Since(a.toastAt) > 2600*time.Millisecond {
		a.toast = ""
		return
	}
	ui.Box(c).Absolute().Left(20).Right(20).Bottom(82).Center().IgnoreInput(true).Children(func() {
		ui.Text(c, a.toast).FontSize(11.5).TextColor(ui.Hex("#ffffff")).TextAlign(ui.Center).
			Padding(8, 13).Radius(10).Background(ui.RGBA(20, 23, 27, 0.94)).Border(1, colors().hairline)
	})
	c.After(2600*time.Millisecond - time.Since(a.toastAt))
}

func (a *app) mobileHeading(c *ui.Context, title, sub string) ui.Element {
	p := colors()
	return ui.Row(c).Height(58).Padding(5, 18, 6, 18).AlignItems(ui.Center).Gap(11).Children(func() {
		ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(23).FontWeight(700).SingleLine()
			if sub != "" {
				ui.Text(c, sub).FontSize(10.5).TextColor(p.faint).SingleLine()
			}
		})
	})
}

func (a *app) mobileSourcePills(c *ui.Context) ui.Element {
	p := colors()
	row := ui.Row(c).FillWidth().Padding(0, 18, 10, 18).Gap(6)
	row.Children(func() {
		for _, src := range []struct {
			index int
			label string
		}{{sourceLocal, "本地视频"}, {sourceWebDAV, "WebDAV"}} {
			on := a.source == src.index
			b := ui.ButtonBase(c).Height(30).PaddingX(12).Radius(18).Gap(6).AlignItems(ui.Center).FocusRing(false).Label(src.label)
			if on {
				b.Background(ui.RGBA(231, 162, 94, 0.15))
				b.TextColor(p.accent)
			} else {
				b.Background(ui.RGBA(255, 255, 255, 0.055))
				b.TextColor(p.muted)
			}
			b.OnClick(func() { a.switchSource(src.index) })
			b.Children(func() {
				ui.Box(c).Size(5, 5).Radius(3).Background(func() ui.Color {
					if on {
						return p.accent
					}
					return p.faint
				}())
				ui.Text(c, src.label).FontSize(11)
			})
		}
		ui.Box(c).Grow(1)
		b := ui.ButtonBase(c).Height(30).PaddingX(10).Radius(18).Gap(5).AlignItems(ui.Center).
			Background(ui.RGBA(255, 255, 255, 0.055)).TextColor(p.muted).Label("来源")
		b.OnClick(func() { a.mobileWebdavSheet = true })
		b.Children(func() {
			ui.Icon(c, iconPlus).Size(13, 13)
			ui.Text(c, "来源").FontSize(11)
		})
	})
	return row
}

func (a *app) mobileHomePage(c *ui.Context) ui.Element {
	p := colors()
	if a.webdav != nil && !a.webdavHasLoaded && !a.webdavBusy {
		a.loadWebDAV()
	}
	root := ui.Column(c).Fill()
	root.Children(func() {
		ui.Row(c).Height(60).Padding(6, 18, 4, 18).AlignItems(ui.Center).Gap(11).Children(func() {
			ui.Box(c).Size(30, 30).Radius(9).Background(p.accent).Center().Children(func() {
				ui.Icon(c, iconPlay).Size(11, 11).TextColor(p.onAccent)
			})
			ui.Column(c).Grow(1).Basis(0).Gap(1).Children(func() {
				ui.Text(c, "MYGO VIDEO").FontSize(9.5).FontWeight(700).
					TextColor(p.faint).LetterSpacing(0.8)
				ui.Text(c, "接着看点什么").FontSize(12).TextColor(p.muted)
			})
			iconButton(c, iconSearch, 17, "搜索", func() {
				a.focusSearch = true
				a.mobileSwitchTab(2)
			})
		})
		a.mobileSourcePills(c)
		ui.Scroll(c).Grow(1).TrackScroll(&a.mobileScrolls[0]).Children(func() {
			ui.Column(c).Padding(8, 18, 30, 18).Gap(24).Children(func() {
				recent := a.mobileRecent(4)
				if len(recent) > 0 {
					a.mobileContinueHero(c, recent[0])
				}
				newest := a.mobileNewest(10)
				if len(newest) > 0 {
					ui.Column(c).Gap(11).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Text(c, "最近添加").FontSize(15).FontWeight(650)
							ui.Text(c, a.mobileLibraryLine()).FontSize(10.5).TextColor(p.faint)
							ui.Box(c).Grow(1)
							ghostButton(c, "全部", nil, func() { a.mobileSwitchTab(1) })
						})
						a.mobileGrid(c, newest)
					})
				}
				a.mobileWebdavCard(c)
				if len(recent) == 0 && len(newest) == 0 {
					a.mobileEmpty(c, "还没有视频", "从系统文件选择器导入本地视频，或在 WebDAV 里连接服务器。", func() {
						primaryButton(c, "导入本地视频", iconImport, a.mobileImport)
					})
				}
			})
		})
	})
	return root
}

func (a *app) mobileContinueHero(c *ui.Context, it media.Item) {
	p := colors()
	prog, _ := a.store.Progress(it.ID)
	ui.Box(c).Height(208).Radius(18).Clip().Background(p.panel).Children(func() {
		ui.Box(c).Fill().Children(func() { a.thumbArt(c, it, 70) }).
			OnClick(func() { a.openDetail(it) })
		ui.Box(c).Fill().Gradient(ui.Transparent, ui.RGBA(7, 8, 10, 0.95), 180)
		ui.Column(c).Absolute().Left(16).Right(16).Bottom(14).Gap(5).Children(func() {
			ui.Text(c, "继续观看 · "+media.HumanDuration(prog.Position)).
				FontSize(9.5).FontWeight(700).TextColor(p.accent)
			ui.Text(c, it.Name).FontSize(23).FontWeight(750).SingleLine()
			ui.Text(c, a.mobileLine(it)).FontSize(10.5).TextColor(p.muted).SingleLine()
			progressLine(c, float32(prog.Fraction()))
			ui.Row(c).Gap(8).Padding(3, 0, 0, 0).Children(func() {
				primaryButton(c, "继续播放", iconPlay, func() { a.play(it, false) })
				ghostButton(c, "详情", nil, func() { a.openDetail(it) })
			})
		})
	})
}

func (a *app) mobileWebdavCard(c *ui.Context) {
	p := colors()
	cfg := a.store.WebDAV()
	card := ui.ButtonBase(c).FillWidth().Padding(12, 13).Radius(14).
		Background(p.panel).Border(1, p.hairline).Gap(11).AlignItems(ui.Center).
		Label("WebDAV")
	card.OnClick(func() { a.mobileWebdavSheet = true })
	card.Children(func() {
		ui.Box(c).Size(36, 36).Radius(10).Background(ui.RGBA(231, 162, 94, 0.13)).Center().Children(func() {
			ui.Icon(c, iconFolder).Size(17, 17).TextColor(p.accent)
		})
		ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
			title := "配置 WebDAV"
			sub := "连接局域网或远程的影片目录"
			if strings.TrimSpace(cfg.URL) != "" {
				title = "WebDAV"
				sub = cfg.URL
				if a.webdavBusy {
					sub = "正在连接…"
				} else if a.webdavErr != "" {
					sub = a.webdavErr
				} else if a.webdavHasLoaded {
					sub = fmt.Sprintf("%s · %d 个视频", cfg.URL, len(a.webdavVideos))
				}
			}
			ui.Text(c, title).FontSize(12.5).FontWeight(600).SingleLine()
			ui.Text(c, sub).FontSize(10).TextColor(p.faint).SingleLine()
		})
		ui.Icon(c, iconForward).Size(14, 14).TextColor(p.faint)
	})
}

func (a *app) mobileLibraryLine() string {
	if a.lib == nil {
		return ""
	}
	if a.lib.Scanning() {
		return "正在扫描…"
	}
	local := a.lib.Count()
	if len(a.webdavVideos) > 0 {
		return fmt.Sprintf("本地 %d · WebDAV %d", local, len(a.webdavVideos))
	}
	return fmt.Sprintf("本地 %d 个", local)
}

func (a *app) mobileLibraryPage(c *ui.Context) ui.Element {
	root := ui.Column(c).Fill()
	root.Children(func() {
		head := a.mobileHeading(c, "片库", a.mobileLibraryLine())
		head.Children(func() {
			if a.source == sourceLocal {
				iconButton(c, iconPlus, 17, "导入本地视频", a.mobileImport)
			}
			if a.source == sourceWebDAV {
				iconButton(c, iconRefresh, 16, "刷新", func() { a.loadWebDAVPath(a.webdavPath, true) })
			}
		})
		a.mobileSourcePills(c)
		switch a.source {
		case sourceWebDAV:
			a.mobileWebdavLibrary(c).Grow(1)
		default:
			a.mobileLocalLibrary(c).Grow(1)
		}
	})
	return root
}

func (a *app) mobileLocalLibrary(c *ui.Context) ui.Element {
	p := colors()
	items := a.groupSeries(a.lib.Items())
	items = a.mobileFilterItems(items)
	root := ui.Column(c).Fill()
	if len(items) == 0 {
		root.Children(func() {
			a.mobileEmpty(c, "片库是空的", "导入本地视频后会自动扫描；文件会复制到应用私有目录。", func() {
				primaryButton(c, "导入本地视频", iconImport, a.mobileImport)
			})
		})
		return root
	}
	root.Children(func() {
		ui.Row(c).Padding(0, 18, 10, 18).Gap(6).Children(func() {
			for i, label := range []string{"全部", "继续观看", "已看完"} {
				on := a.mobileFilter == i
				b := ui.ButtonBase(c).Height(28).PaddingX(11).Radius(9).Label(label).FocusRing(false)
				if on {
					b.Background(ui.RGBA(255, 255, 255, 0.12)).TextColor(p.text)
				} else {
					b.Background(ui.RGBA(255, 255, 255, 0.05)).TextColor(p.muted)
				}
				b.OnClick(func() { a.mobileFilter = i })
				b.Children(func() { ui.Text(c, label).FontSize(10.5) })
			}
		})
		ui.Scroll(c).Grow(1).TrackScroll(&a.mobileScrolls[1]).Children(func() {
			ui.Column(c).Padding(2, 18, 28, 18).Children(func() {
				a.mobileGrid(c, items)
			})
		})
	})
	return root
}

func (a *app) mobileFilterItems(items []media.Item) []media.Item {
	if a.mobileFilter == 0 {
		return items
	}
	var out []media.Item
	for _, it := range items {
		prog, ok := a.store.Progress(it.ID)
		switch a.mobileFilter {
		case 1:
			if ok && prog.Started() {
				out = append(out, it)
			}
		case 2:
			if ok && prog.Done {
				out = append(out, it)
			}
		}
	}
	return out
}

func (a *app) mobileWebdavLibrary(c *ui.Context) ui.Element {
	p := colors()
	if a.webdav == nil {
		return a.mobileEmpty(c, "还没有配置 WebDAV", "填入服务器地址、用户名和密码，就能浏览服务器上的视频。", func() {
			primaryButton(c, "去设置", iconSettings, func() { a.mobileWebdavSheet = true })
		})
	}
	if !a.webdavHasLoaded && !a.webdavBusy {
		a.loadWebDAV()
	}
	root := ui.Column(c).Fill()
	root.Children(func() {
		ui.Row(c).Padding(0, 18, 10, 18).Gap(8).AlignItems(ui.Center).Children(func() {
			path := "/" + a.webdavPath
			if a.webdavPath == "" {
				path = "/ 根目录"
			}
			if a.webdavPath != "" {
				iconButton(c, iconBack, 15, "上一级", func() { a.openWebDAVPath(parentPath(a.webdavPath)) })
			}
			ui.Text(c, path).FontSize(10.5).TextColor(p.muted).SingleLine().Shrink(1)
			ui.Box(c).Grow(1)
			if a.webdavScanning {
				ui.Text(c, fmt.Sprintf("扫描中 %d", a.webdavFound)).FontSize(10).TextColor(p.faint)
			}
		})
		if a.webdavErr != "" {
			root.Children(func() {
				a.mobileEmpty(c, "连不上 WebDAV", a.webdavErr, func() {
					primaryButton(c, "重试", iconRefresh, func() { a.loadWebDAVPath(a.webdavPath, true) })
				})
			})
			return
		}
		if a.webdavBusy && !a.webdavHasLoaded {
			root.Children(func() { a.mobileEmpty(c, "正在载入…", "", nil) })
			return
		}
		var folders []webdavEntry
		for _, e := range a.webdavEntries {
			if e.Dir {
				folders = append(folders, webdavEntry{e.Name, e.Path})
			}
		}
		ui.Scroll(c).Grow(1).TrackScroll(&a.mobileScrolls[1]).Children(func() {
			ui.Column(c).Padding(2, 18, 28, 18).Gap(16).Children(func() {
				if len(folders) > 0 {
					ui.Column(c).Gap(6).Children(func() {
						for _, f := range folders {
							a.mobileFolderRow(c, f.name, f.path)
						}
					})
				}
				if len(a.webdavVideos) > 0 {
					ui.Column(c).Gap(10).Children(func() {
						ui.Text(c, "视频").FontSize(13).FontWeight(600)
						a.mobileGrid(c, a.groupSeries(a.webdavVideos))
					})
				}
				if len(folders) == 0 && len(a.webdavVideos) == 0 {
					a.mobileEmpty(c, "这个文件夹是空的", "", nil)
				}
			})
		})
	})
	return root
}

type webdavEntry struct {
	name string
	path string
}

func (a *app) mobileFolderRow(c *ui.Context, name, path string) {
	p := colors()
	b := ui.ButtonBase(c).FillWidth().Height(48).PaddingX(12).Radius(12).
		Background(p.panel).Border(1, p.hairline).Gap(10).AlignItems(ui.Center).Label(name)
	b.OnClick(func() { a.openWebDAVPath(path) })
	b.Children(func() {
		ui.Icon(c, iconFolder).Size(16, 16).TextColor(p.faint)
		ui.Text(c, name).FontSize(12).TextColor(p.text).SingleLine().Shrink(1)
		ui.Box(c).Grow(1)
		ui.Icon(c, iconForward).Size(13, 13).TextColor(p.faint)
	})
}

func (a *app) mobileSearchPage(c *ui.Context) ui.Element {
	p := colors()
	root := ui.Column(c).Fill()
	root.Children(func() {
		a.mobileHeading(c, "搜索", "本地视频与 WebDAV")
		ui.Row(c).Padding(0, 18, 10, 18).Gap(9).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconSearch).Size(17, 17).TextColor(p.faint)
			input := ui.TextInput(c, &a.query).Placeholder("搜索文件名、剧名或 S01E01").
				TextColor(p.text).Height(42).Grow(1)
			input.Bind(&a.searchBox)
			input.Keyboard(ui.KeyboardSearch).ReturnKey(ui.ReturnSearch)
			if a.focusSearch {
				a.focusSearch = false
				a.searchBox.Focus(c)
			}
		})
		ui.Row(c).Padding(0, 18, 12, 18).Gap(6).Children(func() {
			for i, label := range []string{"全部来源", "本地", "WebDAV"} {
				on := a.mobileSearchSource == i
				b := ui.ButtonBase(c).Height(28).PaddingX(10).Radius(8).Label(label).FocusRing(false)
				if on {
					b.Background(ui.RGBA(231, 162, 94, 0.15)).TextColor(p.accent)
				} else {
					b.Background(ui.RGBA(255, 255, 255, 0.05)).TextColor(p.muted)
				}
				b.OnClick(func() { a.mobileSearchSource = i })
				b.Children(func() { ui.Text(c, label).FontSize(10.5) })
			}
		})
		a.mobileSearchBody(c).Grow(1)
	})
	return root
}

func (a *app) mobileSearchBody(c *ui.Context) ui.Element {
	p := colors()
	if strings.TrimSpace(a.query) == "" {
		return a.mobileEmpty(c, "搜点什么", "输入文件名、剧名或集数，比如「雾港」或「S02E05」。", nil)
	}
	results := a.mobileSearchResults()
	root := ui.Column(c).Fill()
	root.Children(func() {
		ui.Row(c).Padding(0, 18, 10, 18).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("找到 %d 个", len(results))).FontSize(12).FontWeight(600)
			if a.webdavScanning {
				ui.Text(c, fmt.Sprintf("WebDAV 扫描中 %d", a.webdavFound)).FontSize(10).TextColor(p.faint)
			}
		})
		if len(results) == 0 {
			root.Children(func() {
				msg := "换个关键词试试。"
				if a.webdavScanning {
					msg = "WebDAV 还在扫描，结果会陆续出现。"
				}
				a.mobileEmpty(c, "没有找到", msg, nil)
			})
			return
		}
		ui.Scroll(c).Grow(1).TrackScroll(&a.mobileScrolls[2]).Children(func() {
			ui.Column(c).Padding(2, 18, 28, 18).Children(func() {
				a.mobileGrid(c, results)
			})
		})
	})
	return root
}

func (a *app) mobileSearchResults() []media.Item {
	q := strings.ToLower(strings.TrimSpace(a.query))
	if q == "" {
		return nil
	}
	seen := map[string]bool{}
	var pool []media.Item
	add := func(items []media.Item) {
		for _, it := range items {
			if !seen[it.ID] {
				seen[it.ID] = true
				pool = append(pool, it)
			}
		}
	}
	if a.mobileSearchSource == 0 || a.mobileSearchSource == 1 {
		add(a.lib.Items())
	}
	if a.mobileSearchSource == 0 || a.mobileSearchSource == 2 {
		if len(a.webdavAllItems) > 0 {
			add(a.webdavAllItems)
		} else {
			add(a.webdavVideos)
		}
		if a.webdav != nil && !a.mobileSearchIndex && !a.webdavScanning {
			a.mobileSearchIndex = true
			a.scanWebDAVAll()
		}
	}
	var out []media.Item
	for _, it := range pool {
		haystack := strings.ToLower(strings.Join([]string{it.Name, it.Series, it.EpisodeLabel(), it.Path, it.RemoteID}, " "))
		if strings.Contains(haystack, q) {
			out = append(out, it)
		}
	}
	return a.groupSeries(out)
}

func (a *app) mobileDetailPage(c *ui.Context) ui.Element {
	p := colors()
	it := a.current
	prog, hasProg := a.store.Progress(it.ID)
	root := ui.Column(c).Fill()
	root.Children(func() {
		ui.Row(c).Height(54).Padding(3, 12, 4, 16).AlignItems(ui.Center).Gap(6).Children(func() {
			iconButton(c, iconBack, 17, "返回", a.mobileBack)
			ui.Text(c, "详情").FontSize(14).FontWeight(600)
			ui.Box(c).Grow(1)
		})
		ui.Scroll(c).Grow(1).TrackScroll(&a.mobilePlayerScroll).Children(func() {
			ui.Column(c).Padding(0, 18, 30, 18).Gap(18).Children(func() {
				ui.Box(c).Height(176).Radius(16).Clip().Background(p.panel).Children(func() {
					ui.Box(c).Fill().Children(func() { a.thumbArt(c, it, 66) })
					ui.Box(c).Fill().Gradient(ui.Transparent, ui.RGBA(8, 9, 12, 0.92), 180)
					ui.Column(c).Absolute().Left(14).Right(14).Bottom(12).Gap(3).Children(func() {
						if label := it.EpisodeLabel(); label != "" {
							ui.Text(c, label).FontSize(9.5).FontWeight(700).TextColor(p.accent)
						}
						ui.Text(c, it.Name).FontSize(24).FontWeight(750).SingleLine()
						ui.Text(c, a.mobileLine(it)).FontSize(10.5).TextColor(p.muted).SingleLine()
					})
				})
				ui.Row(c).Gap(8).Children(func() {
					label := "播放"
					if hasProg && prog.Started() {
						label = "继续观看 · " + media.HumanDuration(prog.Position)
					}
					primaryButton(c, label, iconPlay, func() { a.play(a.detailPlayTarget(it), false) })
					ghostButton(c, "从头", iconRefresh, func() { a.play(a.detailPlayTarget(it), true) })
					ghostButton(c, "片库", iconLibrary, func() { a.mobileSwitchTab(1) })
				})
				eps := a.mobileSeriesOf(it)
				if len(eps) > 1 {
					ui.Column(c).Gap(8).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Text(c, "选集").FontSize(13.5).FontWeight(600)
							ui.Text(c, fmt.Sprintf("%d 集", len(eps))).FontSize(10).TextColor(p.faint)
						})
						ui.ScrollHorizontal(c).Height(62).Gap(8).Children(func() {
							for _, ep := range eps {
								a.mobileEpisodeChip(c, ep, ep.ID == it.ID)
							}
						})
					})
				}
				if hasProg && prog.Position > 0 {
					ui.Column(c).Gap(8).Padding(13).Radius(13).Background(p.panel).Border(1, p.hairline).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Text(c, "观看进度").FontSize(12).FontWeight(600)
							ui.Text(c, media.HumanDuration(prog.Position)+" / "+media.HumanDuration(prog.Duration)).
								FontSize(10).TextColor(p.muted)
							ui.Box(c).Grow(1)
							if at := finished(prog.Updated); at != "" {
								ui.Text(c, at).FontSize(9.5).TextColor(p.faint)
							}
						})
						progressLine(c, float32(prog.Fraction()))
					})
				}
				ui.Column(c).Gap(7).Padding(13).Radius(13).Background(p.panel).Border(1, p.hairline).Children(func() {
					ui.Text(c, "文件信息").FontSize(12).FontWeight(600)
					switch it.Source {
					case "webdav":
						fact(c, "来源", "WebDAV")
						fact(c, "大小", media.HumanSize(it.Size))
						fact(c, "位置", it.RemoteID)
					default:
						fact(c, "格式", strings.ToUpper(strings.TrimPrefix(it.Ext, ".")))
						fact(c, "大小", media.HumanSize(it.Size))
						fact(c, "位置", it.Path)
					}
					fact(c, "解码", "系统硬件解码 · 自动")
				})
				ui.Text(c, "仅播放你有权访问的内容。").FontSize(10).TextColor(p.faint)
			})
		})
	})
	return root
}

func (a *app) mobileEpisodeChip(c *ui.Context, ep media.Item, current bool) {
	p := colors()
	prog, _ := a.store.Progress(ep.ID)
	b := ui.ButtonBase(c).Width(84).Height(60).Shrink(0).Padding(7, 8).Radius(10).
		Column().Gap(2).AlignItems(ui.Stretch).FocusRing(false).Label(ep.EpisodeLabel())
	if current {
		b.Background(ui.RGBA(231, 162, 94, 0.12)).Border(1, ui.RGBA(231, 162, 94, 0.5))
	} else {
		b.Background(p.panel).Border(1, p.hairline)
	}
	b.OnClick(func() { a.play(ep, false) })
	b.Children(func() {
		label := ep.EpisodeLabel()
		if label == "" {
			label = fmt.Sprintf("第 %d 集", ep.Episode)
		}
		color := p.muted
		if current {
			color = p.accent
		}
		ui.Text(c, label).FontSize(9.5).FontWeight(700).TextColor(color)
		status := fmt.Sprintf("第 %d 集", ep.Episode)
		if prog.Done {
			status = "已看完"
		} else if prog.Started() {
			status = fmt.Sprintf("已看 %d%%", max(int(prog.Fraction()*100+0.5), 1))
		}
		ui.Text(c, status).FontSize(9).TextColor(p.faint).SingleLine()
	})
}

func (a *app) mobileMinePage(c *ui.Context) ui.Element {
	p := colors()
	cfg := a.store.WebDAV()
	root := ui.Column(c).Fill()
	root.Children(func() {
		a.mobileHeading(c, "我的", "本地 + WebDAV · 4K 硬解优先")
		ui.Scroll(c).Grow(1).TrackScroll(&a.mobileScrolls[3]).Children(func() {
			ui.Column(c).Padding(0, 18, 28, 18).Gap(20).Children(func() {
				ui.Row(c).Padding(13).Radius(16).Background(p.panel).Border(1, p.hairline).Gap(11).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(44, 44).Radius(14).Background(p.accent).Center().Children(func() {
						ui.Text(c, "M").FontSize(18).FontWeight(800).TextColor(p.onAccent)
					})
					ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
						ui.Text(c, "MyGo Video").FontSize(13.5).FontWeight(650)
						ui.Text(c, fmt.Sprintf("本地 %d 个视频", a.lib.Count())).FontSize(10).TextColor(p.faint)
					})
				})
				ui.Column(c).Gap(7).Children(func() {
					ui.Text(c, "内容来源").FontSize(13).FontWeight(600).PaddingX(2)
					a.mobileMineRow(c, iconFolder, "本地视频", fmt.Sprintf("应用私有目录 · %d 个", a.lib.Count()), func() {
						a.switchSource(sourceLocal)
						a.mobileSwitchTab(1)
					})
					desc := "未配置"
					if strings.TrimSpace(cfg.URL) != "" {
						desc = cfg.URL
						if a.webdavErr != "" {
							desc = a.webdavErr
						}
					}
					a.mobileMineRow(c, iconFolder, "WebDAV", desc, func() { a.mobileWebdavSheet = true })
					a.mobileMineRow(c, iconImport, "导入本地视频", "从系统文件选择器复制到片库", a.mobileImport)
				})
				if a.mobileImportBusy || a.mobileImportText != "" {
					ui.Column(c).Gap(5).Padding(12).Radius(12).Background(ui.RGBA(231, 162, 94, 0.08)).Children(func() {
						if a.mobileImportBusy {
							ui.Spinner(c).Size(16, 16)
						}
						ui.Text(c, a.mobileImportText).FontSize(10.5).TextColor(p.muted)
					})
				}
				ui.Column(c).Gap(7).Children(func() {
					ui.Text(c, "播放").FontSize(13).FontWeight(600).PaddingX(2)
					a.mobileMineToggle(c, iconPlay, "播放时保持亮屏", "暂停或退出后释放",
						!a.store.KeepAwakeOff(), a.toggleMobileKeepAwake)
					a.mobileMineRow(c, iconRefresh, "默认播放速度", rateLabel(a.store.Rate())+" · 每个视频可单独调整",
						a.cycleMobileDefaultRate)
				})
				ui.Column(c).Gap(7).Children(func() {
					ui.Text(c, "关于").FontSize(13).FontWeight(600).PaddingX(2)
					a.mobileMineRow(c, iconSettings, "版本", version()+" · Android "+androidABI(), nil)
					a.mobileMineRow(c, iconFolder, "数据目录", a.dataDir, nil)
				})
				ui.Text(c, "仅播放你有权访问的内容。").FontSize(9.5).TextColor(p.faint).TextAlign(ui.Center).FillWidth()
			})
		})
	})
	return root
}

func (a *app) mobileMineRow(c *ui.Context, icon *ui.SVG, title, sub string, onClick func()) {
	p := colors()
	b := ui.ButtonBase(c).FillWidth().Padding(11, 12).Radius(12).
		Background(p.panel).Border(1, p.hairline).Gap(10).AlignItems(ui.Center).Label(title)
	if onClick != nil {
		b.OnClick(onClick)
	}
	b.Children(func() {
		ui.Box(c).Size(32, 32).Radius(9).Background(ui.RGBA(255, 255, 255, 0.06)).Center().Children(func() {
			ui.Icon(c, icon).Size(16, 16).TextColor(p.muted)
		})
		ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(12).FontWeight(550)
			if sub != "" {
				ui.Text(c, sub).FontSize(9.5).TextColor(p.faint).SingleLine()
			}
		})
		if onClick != nil {
			ui.Icon(c, iconForward).Size(13, 13).TextColor(p.faint)
		}
	})
}

func (a *app) mobileMineToggle(c *ui.Context, icon *ui.SVG, title, sub string, on bool, onClick func()) {
	p := colors()
	b := ui.ButtonBase(c).FillWidth().Padding(11, 12).Radius(12).
		Background(p.panel).Border(1, p.hairline).Gap(10).AlignItems(ui.Center).
		Label(title).FocusRing(false)
	if onClick != nil {
		b.OnClick(onClick)
	}
	b.Children(func() {
		ui.Box(c).Size(32, 32).Radius(9).Background(ui.RGBA(255, 255, 255, 0.06)).Center().Children(func() {
			ui.Icon(c, icon).Size(16, 16).TextColor(p.muted)
		})
		ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(12).FontWeight(550)
			ui.Text(c, sub).FontSize(9.5).TextColor(p.faint).SingleLine()
		})
		ui.Box(c).Size(38, 22).Radius(12).Background(func() ui.Color {
			if on {
				return p.accent
			}
			return ui.RGBA(255, 255, 255, 0.13)
		}()).Children(func() {
			ui.Box(c).Absolute().Right(3).Top(3).Size(16, 16).Radius(8).Background(ui.Hex("#ffffff"))
		})
	})
}

func (a *app) toggleMobileKeepAwake() {
	off := !a.store.KeepAwakeOff()
	a.store.SetKeepAwakeOff(off)
	if off {
		a.releaseDisplayLockNow()
		a.say("播放时不再保持亮屏")
	} else {
		if a.playerPlaying() {
			a.keepDisplayOn()
		}
		a.say("播放时保持亮屏")
	}
	a.invalidate()
}

func (a *app) cycleMobileDefaultRate() {
	next := rates[0]
	current := a.store.Rate()
	for i, r := range rates {
		if current == r {
			next = rates[(i+1)%len(rates)]
			break
		}
	}
	a.store.SetRate(next)
	a.say("默认播放速度 %s", rateLabel(next))
	a.invalidate()
}

func (a *app) mobileSheet(c *ui.Context) {
	mobile.Sheet(c, &a.mobileWebdavSheet, mobile.SheetOptions{
		Detents: []float32{0.86}, Label: "WebDAV 设置",
	}, func() {
		p := colors()
		ui.Scroll(c).Grow(1).Basis(0).Children(func() {
			ui.Column(c).Padding(4, 18, 24, 18).Gap(12).Children(func() {
				ui.Text(c, "WebDAV").FontSize(21).FontWeight(700)
				ui.Text(c, "连接局域网或远程服务器。只保存地址与账号，播放经过本机代理。").
					FontSize(11).TextColor(p.muted)
				settingsField(c, "服务器地址", &a.webdavURL, "http://192.168.31.95:5244/dav")
				settingsField(c, "用户名", &a.webdavUser, "可留空")
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, "密码").FontSize(11).TextColor(p.muted)
					ui.TextInput(c, &a.webdavPass).Password().Height(38)
				})
				if a.webdavErr != "" {
					ui.Text(c, a.webdavErr).FontSize(10).TextColor(ui.Hex("#ef8f7a"))
				}
				ui.Row(c).Gap(8).Padding(4, 0, 0, 0).Children(func() {
					primaryButton(c, "保存并连接", iconCheck, func() {
						a.saveWebDAV()
						if a.webdavErr == "" {
							a.mobileWebdavSheet = false
							a.openWebDAVPath("")
							a.loadWebDAVPath("", true)
						}
					})
					ghostButton(c, "关闭", nil, func() { a.mobileWebdavSheet = false })
				})
				ui.Text(c, "明文 HTTP 只在局域网需要。播放 4K 时由系统 MediaPlayer 硬解，不转码。").
					FontSize(9.5).TextColor(p.faint)
			})
		})
	})
	mobile.Sheet(c, &a.mobileEpisodes, mobile.SheetOptions{
		Detents: []float32{0.7}, Label: "选集",
	}, func() {
		eps := a.mobileSeriesOf(a.playing)
		ui.Scroll(c).Grow(1).Basis(0).Children(func() {
			ui.Column(c).Padding(4, 18, 24, 18).Gap(6).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "选集").FontSize(20).FontWeight(700)
					ui.Text(c, fmt.Sprintf("%d 集", len(eps))).FontSize(10).TextColor(colors().faint)
				})
				for _, ep := range eps {
					ep := ep
					row := mobile.ListRow(c.Key(ep.ID), mobile.RowOptions{
						Title:      ep.EpisodeLabel() + " · 第 " + fmt.Sprint(ep.Episode) + " 集",
						Subtitle:   a.mobileLine(ep),
						Disclosure: ep.ID == a.playing.ID,
					})
					if row.Clicked() {
						a.mobileEpisodes = false
						a.play(ep, false)
					}
				}
			})
		})
	})
}

func (a *app) mobileEmpty(c *ui.Context, title, sub string, action func()) ui.Element {
	p := colors()
	box := ui.Column(c).FillWidth().PaddingY(64).AlignItems(ui.Center).Justify(ui.Center).Gap(9)
	box.Children(func() {
		ui.Icon(c, iconFilm).Size(30, 30).TextColor(p.faint)
		ui.Text(c, title).FontSize(14).FontWeight(600)
		if sub != "" {
			ui.Text(c, sub).FontSize(11).TextColor(p.muted).TextAlign(ui.Center).MaxWidth(300)
		}
		if action != nil {
			action()
		}
	})
	return box
}

func (a *app) mobileGrid(c *ui.Context, items []media.Item) {
	for i := 0; i < len(items); i += 2 {
		end := min(i+2, len(items))
		ui.Row(c).Gap(12).Children(func() {
			for _, it := range items[i:end] {
				a.mobileVideoCard(c, it)
			}
			if end-i < 2 {
				ui.Box(c).Grow(1)
			}
		})
	}
}

func (a *app) mobileVideoCard(c *ui.Context, it media.Item) {
	p := colors()
	prog, hasProg := a.store.Progress(it.ID)
	b := ui.ButtonBase(c).Grow(1).Basis(0).Padding(4, 4).Radius(12).
		Column().AlignItems(ui.Stretch).Gap(6).FocusRing(false).Label(it.Name)
	b.OnClick(func() { a.openDetail(it) })
	b.Children(func() {
		ui.Box(c).AspectRatio(16.0 / 9).Radius(10).Clip().Background(p.bg).Children(func() {
			a.thumbArt(c, it, 24)
			ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(3).
				Background(ui.RGBA(255, 255, 255, 0.18)).Children(func() {
				if hasProg && prog.Duration > 0 {
					ui.Box(c).Grow(float32(prog.Fraction())).Height(3).Background(p.accent)
				}
			})
			if it.Source == "webdav" {
				ui.Text(c, "WebDAV").Absolute().Left(7).Top(6).FontSize(8).FontWeight(700).
					TextColor(ui.RGBA(255, 255, 255, 0.78)).Padding(2, 5).Radius(4).
					Background(ui.RGBA(0, 0, 0, 0.5))
			}
			if hasProg && prog.Started() {
				ui.Text(c, remaining(prog)).Absolute().Right(7).Bottom(6).
					FontSize(9).FontWeight(600).TextColor(ui.RGBA(255, 255, 255, 0.84))
			}
		})
		ui.Text(c, it.Name).FontSize(11.5).FontWeight(600).SingleLine().TextColor(p.text)
		ui.Text(c, a.mobileLine(it)).FontSize(9.5).TextColor(p.faint).SingleLine()
	})
}

func (a *app) mobileLine(it media.Item) string {
	if it.Series != "" {
		line := it.Series
		if label := it.EpisodeLabel(); label != "" {
			line += " · " + label
		}
		return line
	}
	return it.Subtitle()
}

func (a *app) mobileNewest(n int) []media.Item {
	items := a.groupSeries(a.mobileKnownItems())
	sort.Slice(items, func(i, j int) bool { return items[i].ModTime.After(items[j].ModTime) })
	if len(items) > n {
		items = items[:n]
	}
	return items
}

func (a *app) mobileRecent(n int) []media.Item {
	items := a.mobileKnownItems()
	type watched struct {
		it   media.Item
		when int64
	}
	var list []watched
	for _, it := range items {
		p, ok := a.store.Progress(it.ID)
		if !ok || !p.Started() {
			continue
		}
		list = append(list, watched{it, p.Updated})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].when > list[j].when })
	if len(list) > n {
		list = list[:n]
	}
	out := make([]media.Item, 0, len(list))
	for _, w := range list {
		out = append(out, w.it)
	}
	return out
}

func (a *app) mobileKnownItems() []media.Item {
	seen := map[string]bool{}
	var out []media.Item
	add := func(items []media.Item) {
		for _, it := range items {
			if !seen[it.ID] {
				seen[it.ID] = true
				out = append(out, it)
			}
		}
	}
	if a.lib != nil {
		add(a.lib.Items())
	}
	add(a.webdavVideos)
	add(a.webdavAllItems)
	return out
}

func (a *app) mobileSeriesOf(it media.Item) []media.Item {
	if it.Series == "" {
		return nil
	}
	var pool []media.Item
	switch it.Source {
	case "webdav":
		pool = append(pool, a.webdavVideos...)
		pool = append(pool, a.webdavAllItems...)
	default:
		if a.lib != nil {
			pool = a.lib.Items()
		}
	}
	seen := map[string]bool{}
	var out []media.Item
	for _, x := range pool {
		if x.Series != it.Series || seen[x.ID] {
			continue
		}
		seen[x.ID] = true
		out = append(out, x)
	}
	if len(out) < 2 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Season != out[j].Season {
			return out[i].Season < out[j].Season
		}
		return out[i].Episode < out[j].Episode
	})
	return out
}

func (a *app) mobileImport() {
	if a.mobileImportBusy {
		return
	}
	a.mobileImportBusy = true
	a.mobileImportText = "正在选择视频…"
	a.invalidate()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:   a.win,
			Title:    "导入本地视频",
			Multiple: true,
			Filters: []mygo.FileFilter{{
				Name:       "视频",
				MIMETypes:  []string{"video/*"},
				Extensions: []string{"mp4", "m4v", "mov", "mkv", "webm", "avi"},
			}},
		})
		if err != nil {
			mygo.RunOnMain(func() {
				a.mobileImportBusy = false
				a.mobileImportText = ""
				a.say("导入失败：%v", err)
			})
			return
		}
		if len(paths) == 0 {
			mygo.RunOnMain(func() {
				a.mobileImportBusy = false
				a.mobileImportText = ""
				a.invalidate()
			})
			return
		}
		mygo.RunOnMain(func() {
			a.mobileImportText = fmt.Sprintf("准备导入 %d 个视频…", len(paths))
			a.invalidate()
		})
		dir := filepath.Join(a.dataDir, "videos")
		_ = os.MkdirAll(dir, 0o700)
		var total int64
		for _, src := range paths {
			if st, err := os.Stat(src); err == nil {
				total += st.Size()
			}
		}
		var done, copied int
		var copiedBytes int64
		for i, src := range paths {
			st, err := os.Stat(src)
			if err != nil || st.IsDir() {
				continue
			}
			name := filepath.Base(src)
			dst := mobileUniquePath(dir, name)
			done++
			prefix := fmt.Sprintf("正在导入 %d/%d · %s", i+1, len(paths), name)
			if err := copyMobileFile(src, dst, st.Size(), func(n int64) {
				mygo.RunOnMain(func() {
					pct := int(float64(copiedBytes+n) / float64(max(total, 1)) * 100)
					a.mobileImportText = fmt.Sprintf("%s · %d%%", prefix, pct)
					a.invalidate()
				})
			}); err != nil {
				mygo.RunOnMain(func() { a.say("导入 %s 失败：%v", name, err) })
				continue
			}
			copied++
			copiedBytes += st.Size()
		}
		mygo.RunOnMain(func() {
			a.mobileImportBusy = false
			a.mobileImportText = fmt.Sprintf("已导入 %d 个视频", copied)
			go a.scan()
			a.say("已导入 %d 个视频", copied)
		})
	}()
}

func mobileUniquePath(dir, name string) string {
	dst := filepath.Join(dir, name)
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		return dst
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func copyMobileFile(src, dst string, size int64, progress func(int64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()
	buf := make([]byte, 1<<20)
	var n int64
	last := time.Now()
	for {
		read, rerr := in.Read(buf)
		if read > 0 {
			if _, werr := out.Write(buf[:read]); werr != nil {
				return werr
			}
			n += int64(read)
			if progress != nil && time.Since(last) > 400*time.Millisecond {
				progress(n)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if progress != nil {
		progress(n)
	}
	return out.Close()
}

func androidABI() string {
	return "arm64"
}
