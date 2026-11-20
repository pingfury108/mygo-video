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

// playNext goes on to the next episode, if the series has one.
func (a *app) playNext() {
	if ep, ok := a.neighbor(a.playing, 1); ok {
		a.play(ep, true)
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
	// A row holds six episodes, and each takes its sixth of the width: a
	// fixed width left the rest of the row empty.
	b := ui.ButtonBase(c).Grow(1).Height(44).Radius(8).PaddingX(10).Gap(8).
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
			switch {
			case prog.Done:
				status = "已看完"
			case prog.Started():
				// A part of one percent reads zero, which tells nothing.
				pct := int(prog.Fraction()*100 + 0.5)
				if pct < 1 {
					pct = 1
				}
				status = fmt.Sprintf("已看 %d%%", pct)
			}
			ui.Text(c, status).FontSize(10.5).TextColor(p.faint).SingleLine()
		})
	})
}

// groupSeries folds the episodes of a series into the one card they
// deserve: the episode the listener is on, or the newest.
func (a *app) groupSeries(items []media.Item) []media.Item {
	out := make([]media.Item, 0, len(items))
	at := map[string]int{}
	for _, it := range items {
		if it.Series == "" {
			out = append(out, it)
			continue
		}
		if i, ok := at[it.Series]; ok {
			if a.betterEpisode(it, out[i]) {
				out[i] = it
			}
			continue
		}
		at[it.Series] = len(out)
		out = append(out, it)
	}
	return out
}

// betterEpisode says whether b is the episode a series' card should show
// instead of a: the one being watched, else the latest.
func (a *app) betterEpisode(b, a1 media.Item) bool {
	pb, okb := a.store.Progress(b.ID)
	pa, oka := a.store.Progress(a1.ID)
	sb := okb && pb.Started()
	sa := oka && pa.Started()
	if sb != sa {
		return sb
	}
	if sb && sa {
		return pb.Updated > pa.Updated
	}
	if b.Season != a1.Season {
		return b.Season > a1.Season
	}
	return b.Episode > a1.Episode
}

// seriesLine is what a series' card says under its name: how many episodes
// there are, and which one the listener is on.
func (a *app) seriesLine(it media.Item) string {
	if it.Series == "" {
		return ""
	}
	eps := a.seriesOf(it)
	if len(eps) < 2 {
		return ""
	}
	if prog, ok := a.store.Progress(it.ID); ok && prog.Started() {
		return fmt.Sprintf("全 %d 集 · 看到 %s", len(eps), it.EpisodeLabel())
	}
	last := eps[len(eps)-1]
	return fmt.Sprintf("全 %d 集 · 更新至 %s", len(eps), last.EpisodeLabel())
}
