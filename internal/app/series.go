package app

import (
	"fmt"
	"sort"

	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
)

// seriesOf lists the episodes of a video's series, in season and episode
// order. A film has none.
func (a *app) seriesOf(it media.Item) []media.Item {
	if it.Series == "" {
		return nil
	}
	var out []media.Item
	for _, x := range a.items() {
		if x.Series == it.Series {
			out = append(out, x)
		}
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

// neighbor is the episode delta away from it, for the player's buttons.
func (a *app) neighbor(it media.Item, delta int) (media.Item, bool) {
	list := a.seriesOf(it)
	for i, x := range list {
		if x.ID != it.ID {
			continue
		}
		if j := i + delta; j >= 0 && j < len(list) {
			return list[j], true
		}
		break
	}
	return media.Item{}, false
}

func (a *app) playNeighbor(delta int) {
	if ep, ok := a.neighbor(a.playing, delta); ok {
		a.play(ep, false)
	}
}

func (a *app) selectEpisode(ep media.Item) {
	a.current = ep
	a.invalidate()
}

// episodeChip is one episode in the detail page's row.
func (a *app) episodeChip(c *ui.Context, ep media.Item, current bool) {
	p := colors()
	prog, _ := a.store.Progress(ep.ID)
	b := ui.ButtonBase(c).Width(118).Height(44).Radius(8).PaddingX(10).Gap(8).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(ep.EpisodeLabel())
	b.Background(p.panel)
	switch {
	case current:
		b.Border(1, p.accent)
	case b.Hovered():
		b.Background(p.hover)
	}
	b.OnClick(func() { a.selectEpisode(ep) })
	b.Children(func() {
		color := p.muted
		if current {
			color = p.accent
		}
		ui.Text(c, ep.EpisodeLabel()).FontSize(10).FontWeight(700).TextColor(color)
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			status := fmt.Sprintf("第 %d 集", ep.Episode)
			if prog.Started() {
				status = fmt.Sprintf("已看 %.0f%%", prog.Fraction()*100)
			}
			ui.Text(c, status).FontSize(10.5).TextColor(p.faint).SingleLine()
		})
	})
}
