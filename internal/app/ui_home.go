package app

import (
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
)

// homePage leads with what the listener was watching, then what is new.
func (a *app) homePage(c *ui.Context) {
	p := colors()
	a.loadPluginHome()
	items := a.items()
	bar := a.dragWindow(headerRow(c, 32))
	bar.Children(func() {
		ui.Text(c, "首页").FontSize(15).FontWeight(600)
		sub := a.sourceName()
		if a.source == sourceIPTV {
			sub = a.iptvLine()
		}
		ui.Text(c, sub).FontSize(12).TextColor(p.faint)
		ui.Box(c).Grow(1)
		if a.source != sourceIPTV {
			iconButton(c, iconSearch, 16, "搜索", func() { a.goPage(pageSearch) })
		} else {
			iconButton(c, iconRefresh, 16, "刷新列表", func() {
				a.iptvLoaded = false
				a.iptvErr = ""
				a.loadIPTV()
			})
		}
		if a.source == sourceLocal {
			iconButton(c, iconRefresh, 16, "重新扫描", func() { go a.scan() })
		}
	})

	switch {
	case a.source == sourceWebDAV:
		a.webdavHome(c)
		return
	case a.source == sourceIPTV:
		a.iptvHome(c)
		return
	case a.source >= sourcePluginBase:
		a.pluginHome(c)
		return
	}

	ui.Scroll(c).Grow(1).TrackScroll(&a.homeScroll).Children(func() {
		ui.Column(c).Padding(8, 32, 40, 32).Gap(26).Children(func() {
			if len(items) == 0 {
				if a.lib.Scanning() || a.lib.Scanned().IsZero() {
					emptyState(c, "正在扫描…", "正在查找视频文件，稍等一下。", nil)
				} else {
					emptyState(c, "还没有视频", "把视频放进 ~/Movies、桌面或下载文件夹，或应用数据目录下的 videos 文件夹，然后重新扫描。", func() {
						primaryButton(c, "重新扫描", iconRefresh, func() { go a.scan() })
					})
				}
				return
			}

			// The one still being watched leads the page, as a hero.
			var lead media.Item
			recent := a.recentItems(6)
			hasLead := len(recent) > 0
			if hasLead {
				lead = recent[0]
				a.continueHero(c, lead)
			}

			newest := a.groupSeries(items)
			if hasLead {
				filtered := make([]media.Item, 0, len(newest))
				for _, it := range newest {
					if it.ID == lead.ID {
						continue
					}
					if lead.Series != "" && it.Series == lead.Series {
						continue
					}
					filtered = append(filtered, it)
				}
				newest = filtered
			}
			if len(newest) > 0 {
				ui.Column(c).Gap(12).Children(func() {
					sectionHead(c, "最近添加", a.libraryLine(), nil)
					n := min(12, len(newest))
					a.cardRow(c, newest[:n])
				})
			}

			if len(items) > 12 {
				ghostButton(c, "查看全部", iconForward, func() { a.goPage(pageLibrary) })
			}
		})
	})
}

// pluginHome is a source's own page, in the sections it gives.
func (a *app) pluginHome(c *ui.Context) {
	tabs := a.pluginTabs()
	if len(tabs) > 1 {
		ui.ScrollHorizontal(c).Height(36).Children(func() {
			ui.Row(c).PaddingX(32).Gap(6).Children(func() {
				for _, t := range tabs {
					a.pluginTabChip(c, t.key, t.label)
				}
			})
		})
	}
	if a.pluginErr != "" {
		ui.Column(c).Grow(1).Children(func() {
			emptyState(c, "这个来源没能载入", a.pluginErr, func() {
				a.pluginBrowse = nil
				a.pluginErr = ""
				a.pluginBusy = false
				a.invalidate()
			})
		})
		return
	}
	b := a.pluginBrowse
	if b == nil {
		ui.Column(c).Grow(1).Children(func() {
			emptyState(c, "正在载入…", "", nil)
		})
		return
	}
	id := a.plugins[a.source-sourcePluginBase].man.ID
	ui.Scroll(c).Grow(1).TrackScroll(&a.homeScroll).Children(func() {
		ui.Column(c).Padding(8, 32, 40, 32).Gap(26).Children(func() {
			if len(b.Sections) == 0 {
				emptyState(c, "这个来源还没有内容", "", nil)
			}
			for _, sec := range b.Sections {
				if len(sec.Videos) == 0 {
					continue
				}
				videos := sec.Videos
				ui.Column(c).Gap(12).Children(func() {
					sectionHead(c, sec.Title, "", nil)
					items := make([]media.Item, 0, len(videos))
					for _, v := range videos {
						items = append(items, itemFromVideo(id, v))
					}
					a.pluginGrid(c, items)
				})
			}
		})
	})
}

// continueHero is the big card of the video watched last: its own wash, its
// name, and the one action that matters.
func (a *app) continueHero(c *ui.Context, it media.Item) {
	p := colors()
	prog, _ := a.store.Progress(it.ID)
	ui.Box(c).Height(326).Radius(12).Clip().Background(p.bg).Children(func() {
		ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Children(func() {
			a.thumbArt(c, it, 40)
		})
		ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).
			Background(ui.RGBA(0, 0, 0, 0.16))
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(220).
			Gradient(ui.Transparent, ui.RGBA(8, 9, 12, 0.96), 180)
		ui.Column(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).
			Padding(30).Justify(ui.End).Gap(10).Children(func() {
			ui.Text(c, "上次看到 "+media.HumanDuration(prog.Position)).
				FontSize(11).FontWeight(700).TextColor(p.accent)
			ui.Text(c, it.Name).FontSize(30).FontWeight(650).SingleLine()
			ui.Text(c, it.Subtitle()).FontSize(12.5).TextColor(p.muted)
			ui.Row(c).Gap(10).AlignItems(ui.Center).PaddingY(4).Children(func() {
				primaryButton(c, "继续观看", iconPlay, func() { a.play(it, false) })
				ghostButton(c, "从头播放", nil, func() { a.play(it, true) })
				ghostButton(c, "详情", nil, func() { a.openDetail(it) })
			})
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				fraction := float32(prog.Fraction())
				ui.Box(c).Grow(1).Height(4).Radius(2).
					Background(ui.RGBA(255, 255, 255, 0.16)).Children(func() {
					ui.Row(c).Fill().Children(func() {
						ui.Box(c).Grow(fraction).Height(4).Background(p.accent)
						ui.Box(c).Grow(1 - fraction)
					})
				})
				ui.Text(c, "还剩 "+media.HumanDuration(prog.Duration-prog.Position)).
					FontSize(11).TextColor(p.muted)
			})
		})
	})
}

// pluginGrid lays a plugin's videos out in rows, filling the width, so a
// page of them takes the room it has.
// gridColumns is how many cards a page fits across: as many as share its
// width at minW each with gap between them, so a wide window shows more of
// them rather than a band of empty space beside a fixed count.
func gridColumns(c *ui.Context, gutter, gap, minW float32) int {
	w, _ := c.Size()
	avail := w - 232 - gutter*2 // the sidebar, and the page's own side gaps
	n := int((avail + gap) / (minW + gap))
	if n < 1 {
		n = 1
	}
	return n
}

// gridRows lays n items out in rows that share the page's width: as many
// columns as it holds at minW each with gap between them, every item taking
// a share, so a wide window shows more of them rather than a band of empty
// space. A row that is not full keeps the shares of the rows above. The
// caller scrolls it.
func (a *app) gridRows(c *ui.Context, n int, gutter, gap, minW float32, item func(i int)) {
	if n == 0 {
		return
	}
	cols := gridColumns(c, gutter, gap, minW)
	if cols > n {
		cols = n
	}
	for i := 0; i < n; i += cols {
		end := min(i+cols, n)
		last := end - i
		ui.Row(c).Gap(gap).Children(func() {
			for k := i; k < end; k++ {
				item(k)
			}
			for k := last; k < cols; k++ {
				ui.Box(c).Grow(1)
			}
		})
	}
}

// pluginGrid lays a section's videos out: as many columns as the page's
// width holds at the smallest card worth showing, each card taking an equal
// share of it, so a wide window shows more of them rather than a band of
// empty space beside four.
func (a *app) pluginGrid(c *ui.Context, items []media.Item) {
	if len(items) == 0 {
		return
	}
	const gap = float32(14)
	a.gridRows(c, len(items), 32, gap, 190, func(i int) {
		a.videoCard(c, items[i], 0)
	})
}

// pluginTabChip is one of a plugin's pages: its home, or its library.
func (a *app) pluginTabChip(c *ui.Context, key, label string) {
	p := colors()
	active := a.pluginTab == key
	b := ui.ButtonBase(c).Height(28).PaddingX(12).Radius(7).Label(label).
		AlignItems(ui.Center).Justify(ui.Center)
	if active {
		b.Background(p.accent)
	} else {
		b.Background(ui.RGBA(255, 255, 255, 0.04))
		if b.Hovered() {
			b.Background(p.hover)
		}
	}
	b.OnClick(func() { a.setPluginTab(key) })
	b.Children(func() {
		color := p.text
		if active {
			color = ui.Hex("#0d0e11")
		}
		ui.Text(c, label).FontSize(11.5).TextColor(color).SingleLine()
	})
}
