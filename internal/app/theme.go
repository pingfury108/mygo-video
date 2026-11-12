package app

import "github.com/egoist/mygo/ui"

// palette is the app's colors: a cinema dark, with an accent that a poster
// would give it.
type palette struct {
	bg        ui.Color
	sidebar   ui.Color
	panel     ui.Color
	text      ui.Color
	muted     ui.Color
	faint     ui.Color
	hairline  ui.Color
	hover     ui.Color
	active    ui.Color
	accent    ui.Color
	onAccent  ui.Color
	cardShade ui.Color
}

func colors() palette {
	return palette{
		bg:        ui.Hex("#0d0e11"),
		sidebar:   ui.Hex("#101216"),
		panel:     ui.Hex("#17191e"),
		text:      ui.Hex("#f2f3f5"),
		muted:     ui.RGBA(235, 235, 245, 0.56),
		faint:     ui.RGBA(235, 235, 245, 0.3),
		hairline:  ui.RGBA(255, 255, 255, 0.07),
		hover:     ui.RGBA(255, 255, 255, 0.06),
		active:    ui.RGBA(255, 255, 255, 0.1),
		accent:    ui.Hex("#e7a25e"),
		onAccent:  ui.Hex("#1b1206"),
		cardShade: ui.RGBA(0, 0, 0, 0.45),
	}
}

// applyTheme makes the window dark, whatever the desktop asks for: a video
// player reads best on black.
func (a *app) applyTheme(c *ui.Context) {
	p := colors()
	t := *ui.DarkTheme()
	t.Background = p.bg
	t.Surface = p.panel
	t.SurfaceHover = ui.Hex("#1e2127")
	t.SurfacePressed = ui.Hex("#262a31")
	t.Border = p.hairline
	t.Text = p.text
	t.TextMuted = p.muted
	t.Accent = p.accent
	t.AccentHover = ui.Hex("#efb175")
	t.AccentPressed = ui.Hex("#d99450")
	t.AccentText = p.onAccent
	t.InactiveAccent = p.accent
	t.Selection = p.accent.Alpha(0.3)
	t.Focus = p.accent.Alpha(0.6)
	t.Radius = 9
	t.Spacing = 4
	c.SetTheme(&t)
	if root := c.Root(); root.Valid() {
		root.Background(p.bg)
	}
}
