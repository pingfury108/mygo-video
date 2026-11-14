package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
)

// iconButton is a borderless button showing an icon, marked under the pointer.
func iconButton(c *ui.Context, icon *ui.SVG, size float32, label string, onClick func()) ui.Element {
	p := colors()
	b := ui.ButtonBase(c).Size(size+18, size+18).Center().Radius(8).Label(label).TextColor(p.muted).FocusRing(false)
	if b.Hovered() {
		b.Background(p.hover)
	}
	if onClick != nil {
		b.OnClick(onClick)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(size, size)
	})
	return b
}

// primaryButton is the one filled button on a page.
func primaryButton(c *ui.Context, label string, icon *ui.SVG, onClick func()) ui.Element {
	p := colors()
	b := ui.ButtonBase(c).Height(36).PaddingX(16).Gap(8).Radius(8).AlignItems(ui.Center).Label(label)
	b.Background(p.accent)
	if b.Hovered() {
		b.Background(ui.Hex("#efb175"))
	}
	b.OnClick(onClick)
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14).TextColor(p.onAccent)
		}
		ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(p.onAccent).SingleLine()
	})
	return b
}

// ghostButton is a quiet button next to a primary one.
func ghostButton(c *ui.Context, label string, icon *ui.SVG, onClick func()) ui.Element {
	p := colors()
	b := ui.ButtonBase(c).Height(36).PaddingX(14).Gap(8).Radius(8).AlignItems(ui.Center).Label(label).TextColor(p.text)
	if b.Hovered() {
		b.Background(p.hover)
	} else {
		b.Background(ui.RGBA(255, 255, 255, 0.06))
	}
	if onClick != nil {
		b.OnClick(onClick)
	}
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14).TextColor(p.muted)
		}
		ui.Text(c, label).FontSize(13).FontWeight(500).SingleLine()
	})
	return b
}

// sectionHead is a row of a heading and what sits at its end.
func sectionHead(c *ui.Context, title, sub string, tail func()) {
	p := colors()
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Text(c, title).FontSize(16).FontWeight(600)
		if sub != "" {
			ui.Text(c, sub).FontSize(11).TextColor(p.faint)
		}
		ui.Box(c).Grow(1)
		if tail != nil {
			tail()
		}
	})
}

// videoCard is a video in a row or a grid: its picture, name and facts.
func (a *app) videoCard(c *ui.Context, it media.Item, width float32) {
	p := colors()
	prog, _ := a.store.Progress(it.ID)
	b := ui.ButtonBase(c).Radius(10).Padding(6).AlignItems(ui.Stretch).FocusRing(false).Label(it.Name)
	if width > 0 {
		b.Width(width).Shrink(0)
	} else {
		b.Grow(1)
	}
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.OnClick(func() {
		if it.Live {
			a.play(it, true)
			return
		}
		a.openDetail(it)
	})
	b.Children(func() {
		ui.Column(c).Grow(1).Gap(7).Children(func() {
			a.thumb(c, it, prog)
			ui.Text(c, it.Name).FontSize(13).FontWeight(600).SingleLine()
			sub := it.Subtitle()
			if line := a.seriesLine(it); line != "" {
				sub = line
			}
			ui.Text(c, sub).FontSize(11).TextColor(p.faint).SingleLine()
		})
	})
}

// thumb is a picture of a video, or the placeholder its file gets until one
// is made: a dark wash in its own hue, its first character, and how far it
// was watched.
func (a *app) thumb(c *ui.Context, it media.Item, prog media.Progress) {
	p := colors()
	ui.Box(c).AspectRatio(16.0 / 9).Radius(7).Clip().Background(p.bg).Children(func() {
		a.thumbArt(c, it, 26)
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(16).
			Gradient(ui.RGBA(0, 0, 0, 0.55), ui.Transparent, 90)
		if prog.Duration > 0 {
			ui.Row(c).Absolute().Left(0).Right(0).Bottom(0).Height(3).Background(ui.RGBA(255, 255, 255, 0.22)).Children(func() {
				ui.Box(c).Grow(float32(prog.Fraction())).Height(3).Background(p.accent)
				ui.Box(c).Grow(float32(1 - prog.Fraction())).Height(3)
			})
		}
		if prog.Started() {
			ui.Text(c, remaining(prog)).Absolute().Right(7).Bottom(6).FontSize(10).FontWeight(600).
				TextColor(ui.RGBA(255, 255, 255, 0.86))
		}
		if it.Live {
			ui.Text(c, "LIVE").Absolute().Left(7).Top(6).FontSize(9.5).FontWeight(700).
				TextColor(ui.Hex("#ffffff")).Padding(2, 6).Radius(4).
				Background(ui.RGBA(239, 59, 59, 0.9))
		}
	})
}

// thumbArt fills its parent with the video's frame, or the wash its name
// gives it, and its first character in it.
func (a *app) thumbArt(c *ui.Context, it media.Item, initialSize float32) {
	p := colors()
	if b := a.thumbnail(it); b != nil {
		ui.Image(c, b).Fill().Fit(ui.Cover)
		return
	}
	if b := a.posterBitmap(it); b != nil {
		ui.Image(c, b).Fill().Fit(ui.Cover)
		return
	}
	base := posterColor(it.ID)
	wash := base.Mix(ui.RGBA(255, 255, 255, 0.5), 0.5)
	ui.Box(c).Fill().RadialGradient(ui.RadialGradient{
		Center: ui.Point{X: 0.76, Y: 0.12},
		Stops:  []ui.GradientStop{{Color: wash, At: 0}, {Color: p.bg, At: 1}},
	}).Center().Children(func() {
		ui.Text(c, initial(it.Name)).FontSize(initialSize).FontWeight(700).
			TextColor(ui.RGBA(255, 255, 255, 0.42))
	})
}

// emptyState fills a page that has nothing to list yet.
func emptyState(c *ui.Context, title, sub string, action func()) {
	p := colors()
	ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(10).PaddingY(80).Children(func() {
		ui.Icon(c, iconFilm).Size(34, 34).TextColor(p.faint)
		ui.Text(c, title).FontSize(15).FontWeight(600)
		if sub != "" {
			ui.Text(c, sub).FontSize(12).TextColor(p.muted).TextAlign(ui.Center).MaxWidth(420)
		}
		if action != nil {
			action()
		}
	})
}

// row is a horizontal run of cards, scrolling sideways.
func (a *app) cardRow(c *ui.Context, items []media.Item, width float32) {
	if len(items) == 0 {
		return
	}
	ui.ScrollHorizontal(c).Height(width*9/16 + 66).Gap(14).Children(func() {
		for _, it := range items {
			a.videoCard(c, it, width)
		}
	})
}

var posterColors = []string{
	"#2c3f4c", "#443528", "#304437", "#452f3c", "#3d3728", "#2c3846", "#463f30", "#2e3c4c",
}

func posterColor(id string) ui.Color {
	n := 0
	for i := 0; i < len(id); i++ {
		n = n*31 + int(id[i])
	}
	if n < 0 {
		n = -n
	}
	return ui.Hex(posterColors[n%len(posterColors)])
}

func initial(name string) string {
	for _, r := range name {
		return string(r)
	}
	return "?"
}

// remaining is what is left of a video, as a card says it.
func remaining(p media.Progress) string {
	left := p.Duration - p.Position
	if left <= 0 {
		return "已看完"
	}
	return "还剩 " + media.HumanDuration(left)
}

// finished says when a progress was kept, in the way a history reads.
func finished(ms int64) string {
	if ms == 0 {
		return ""
	}
	t := time.UnixMilli(ms)
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "今天 " + t.Format("15:04")
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "昨天 " + t.Format("15:04")
	default:
		return t.Format("1月2日")
	}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
