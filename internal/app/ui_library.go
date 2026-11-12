package app

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
)

// libraryPage is every video in a grid.
func (a *app) libraryPage(c *ui.Context) {
	p := colors()
	items := a.lib.Items()
	ui.Row(c).Height(56).PaddingX(32).AlignItems(ui.Center).Gap(10).DragWindow().Children(func() {
		ui.Text(c, "片库").FontSize(15).FontWeight(600)
		ui.Text(c, a.libraryLine()).FontSize(12).TextColor(p.faint)
		ui.Box(c).Grow(1)
		iconButton(c, iconRefresh, 16, "重新扫描", func() { go a.scan() })
	})

	if len(items) == 0 {
		ui.Column(c).Grow(1).Children(func() {
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

	ui.GridView(c, &a.libraryGrid, len(items), 190, 176, func(i int) {
		a.videoCard(c, items[i], 0)
	}).Grow(1).Padding(8, 28, 36, 28)
}

// searchPage finds videos by name, series or episode.
func (a *app) searchPage(c *ui.Context) {
	p := colors()
	ui.Row(c).Height(56).PaddingX(32).AlignItems(ui.Center).Gap(10).DragWindow().Children(func() {
		ui.Text(c, "搜索").FontSize(15).FontWeight(600)
		ui.Box(c).Grow(1)
	})

	ui.Column(c).Grow(1).PaddingX(32).Gap(16).Children(func() {
		ui.Row(c).Children(func() {
			input := ui.TextInput(c, &a.query).Placeholder("搜索文件名、剧名或 S02E05").TextColor(p.text).Height(38).Grow(1)
			input.Bind(&a.searchBox)
		})
		if a.focusSearch {
			a.focusSearch = false
			a.searchBox.Focus(c)
		}
		results := a.results()
		switch {
		case strings.TrimSpace(a.query) == "":
			ui.Column(c).Gap(9).PaddingY(46).Children(func() {
				ui.Icon(c, iconSearch).Size(26, 26).TextColor(p.faint)
				ui.Text(c, "搜点什么").FontSize(15).FontWeight(600)
				ui.Text(c, "输入文件名、剧名或集数，比如「雾港」或「S02E05」。").
					FontSize(12).TextColor(p.muted)
			})
		case len(results) == 0:
			ui.Column(c).Gap(9).PaddingY(46).Children(func() {
				ui.Icon(c, iconFilm).Size(26, 26).TextColor(p.faint)
				ui.Text(c, "没有找到").FontSize(15).FontWeight(600)
				ui.Text(c, "换个关键词试试。").FontSize(12).TextColor(p.muted)
			})
		default:
			ui.Text(c, "找到 "+strconv.Itoa(len(results))+" 个").FontSize(11).TextColor(p.faint)
			ui.GridView(c, nil, len(results), 190, 176, func(i int) {
				a.videoCard(c, results[i], 0)
			}).Grow(1)
		}
	})
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
