package app

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
)

// libraryPage is every video in a grid.
func (a *app) libraryPage(c *ui.Context) {
	p := colors()
	items := a.groupSeries(a.items())
	bar := a.dragWindow(headerRow(c, 32))
	bar.Children(func() {
		ui.Text(c, "片库").FontSize(15).FontWeight(600)
		ui.Text(c, a.libraryLine()).FontSize(12).TextColor(p.faint)
		ui.Box(c).Grow(1)
		if a.source == sourceLocal {
			iconButton(c, iconRefresh, 16, "重新扫描", func() { go a.scan() })
		}
	})

	if len(items) == 0 {
		ui.Column(c).Grow(1).Children(func() {
			if a.source == sourceIPTV {
				emptyState(c, "直播在首页", "按分组浏览频道，点开就能看。", nil)
				return
			}
			if a.source == sourceWebDAV {
				emptyState(c, "WebDAV 还没有东西", "在设置里检查地址、账号，或在首页先打开一个文件夹。", nil)
				return
			}
			if a.source >= 2 {
				emptyState(c, "这个来源还没有内容", "", nil)
				return
			}
			if a.lib.Scanning() || a.lib.Scanned().IsZero() {
				emptyState(c, "正在扫描…", "正在查找视频文件，稍等一下。", nil)
				return
			}
			emptyState(c, "片库是空的", "把视频放进 ~/Movies、桌面或下载文件夹，然后重新扫描。", func() {
				primaryButton(c, "重新扫描", iconRefresh, func() { go a.scan() })
			})
		})
		return
	}

	ui.Scroll(c).Grow(1).TrackScroll(&a.libraryScroll).Children(func() {
		ui.Column(c).Padding(8, 32, 36, 32).Children(func() {
			a.gridRows(c, len(items), 32, 14, 190, func(i int) {
				a.videoCard(c, items[i], 0)
			})
		})
	})
}

// searchPage finds videos by name, series or episode.
func (a *app) searchPage(c *ui.Context) {
	p := colors()
	bar := a.dragWindow(headerRow(c, 32))
	bar.Children(func() {
		ui.Text(c, "搜索").FontSize(15).FontWeight(600)
		ui.Box(c).Grow(1)
	})

	ui.Column(c).Grow(1).PaddingX(32).Gap(16).Children(func() {
		placeholder := "搜索文件名、剧名或 S02E05"
		switch {
		case a.source == sourceWebDAV:
			placeholder = "搜索这个文件夹里的文件名"
		case a.source >= sourcePluginBase:
			placeholder = "搜索这个来源里的剧集"
		}
		ui.Row(c).Children(func() {
			input := ui.TextInput(c, &a.query).Placeholder(placeholder).TextColor(p.text).Height(38).Grow(1)
			input.Bind(&a.searchBox)
		})
		if a.focusSearch {
			a.focusSearch = false
			a.searchBox.Focus(c)
		}
		a.searchPlugin()
		results := a.searchResults()
		switch {
		case strings.TrimSpace(a.query) == "":
			ui.Column(c).Gap(9).PaddingY(46).Children(func() {
				ui.Icon(c, iconSearch).Size(26, 26).TextColor(p.faint)
				ui.Text(c, "搜点什么").FontSize(15).FontWeight(600)
				ui.Text(c, "输入文件名、剧名或集数，比如「雾港」或「S02E05」。").
					FontSize(12).TextColor(p.muted)
			})
		case a.searchBusy && len(results) == 0:
			ui.Column(c).Gap(9).PaddingY(46).Children(func() {
				ui.Icon(c, iconSearch).Size(26, 26).TextColor(p.faint)
				ui.Text(c, "正在搜索…").FontSize(15).FontWeight(600)
			})
		case len(results) == 0:
			ui.Column(c).Gap(9).PaddingY(46).Children(func() {
				ui.Icon(c, iconFilm).Size(26, 26).TextColor(p.faint)
				ui.Text(c, "没有找到").FontSize(15).FontWeight(600)
				ui.Text(c, "换个关键词试试。").FontSize(12).TextColor(p.muted)
			})
		default:
			ui.Text(c, "找到 "+strconv.Itoa(len(results))+" 个").FontSize(11).TextColor(p.faint)
			ui.Scroll(c).Grow(1).TrackScroll(&a.searchScroll).Children(func() {
				a.gridRows(c, len(results), 32, 14, 190, func(i int) {
					a.videoCard(c, results[i], 0)
				})
			})
		}
	})
}

// searchPlugin asks the source being shown, off the main thread, once for
// each query.
func (a *app) searchPlugin() {
	if a.source < sourcePluginBase || a.searchBusy {
		return
	}
	q := strings.TrimSpace(a.query)
	if q == "" {
		a.pluginSearch = nil
		a.searchQuery = ""
		return
	}
	if q == a.searchQuery {
		return
	}
	pe := a.plugins[a.source-sourcePluginBase]
	p := pe.p
	if p == nil {
		return
	}
	id := pe.man.ID
	a.searchQuery = q
	a.searchBusy = true
	go func() {
		r, err := p.Search(q, 0)
		mygo.RunOnMain(func() {
			a.searchBusy = false
			if a.source < sourcePluginBase || a.plugins[a.source-sourcePluginBase].man.ID != id {
				return
			}
			a.pluginSearch = nil
			if err != nil {
				a.say("%v", err)
				return
			}
			for _, v := range r.Videos {
				a.pluginSearch = append(a.pluginSearch, itemFromVideo(id, v))
			}
			a.invalidate()
		})
	}()
}

func (a *app) searchResults() []media.Item {
	switch {
	case a.source == sourceWebDAV:
		q := strings.ToLower(strings.TrimSpace(a.query))
		if q == "" {
			return nil
		}
		var out []media.Item
		for _, it := range a.webdavVideos {
			if strings.Contains(strings.ToLower(it.Name), q) {
				out = append(out, it)
			}
		}
		return a.groupSeries(out)
	case a.source == sourceIPTV:
		return nil
	case a.source >= sourcePluginBase:
		return a.groupSeries(a.pluginSearch)
	default:
		return a.groupSeries(a.results())
	}
}

func (a *app) results() []media.Item {
	q := strings.ToLower(strings.TrimSpace(a.query))
	if q == "" {
		return nil
	}
	var out []media.Item
	for _, it := range a.lib.Items() {
		haystack := strings.ToLower(strings.Join([]string{it.Name, it.Series, it.EpisodeLabel(), it.Path}, " "))
		if strings.Contains(haystack, q) {
			out = append(out, it)
		}
	}
	return out
}
