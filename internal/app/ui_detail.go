package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
)

// detailPage is one video: what it is, where it stands, and how to play it.
func (a *app) detailPage(c *ui.Context) {
	p := colors()
	it := a.current
	prog, hasProg := a.store.Progress(it.ID)

	ui.Row(c).Height(56).PaddingX(32).AlignItems(ui.Center).Gap(10).DragWindow().Children(func() {
		iconButton(c, iconBack, 16, "返回", func() {
			if len(a.history) > 0 {
				a.back()
			} else {
				a.goPage(pageHome)
			}
		})
		ui.Text(c, "详情").FontSize(15).FontWeight(600)
		ui.Box(c).Grow(1)
	})

	ui.Scroll(c).Grow(1).TrackScroll(&a.detailScroll).Children(func() {
		ui.Column(c).Padding(8, 32, 40, 32).Gap(20).Children(func() {
			// The hero: the video's own color, its name and the one action
			// that matters.
			ui.Box(c).Radius(12).Padding(28).RadialGradient(ui.RadialGradient{
				Center: ui.Point{X: 0.8, Y: 0.12},
				Stops: []ui.GradientStop{
					{Color: posterColor(it.ID).Mix(ui.RGBA(255, 255, 255, 0.35), 0.4), At: 0},
					{Color: p.bg, At: 0.8},
				},
			}).Children(func() {
				ui.Row(c).Fill().Gap(26).AlignItems(ui.End).Children(func() {
					// The video's frame, cropped to a poster, or its wash.
					ui.Box(c).Width(116).Height(174).Radius(8).Clip().Background(p.bg).Children(func() {
						a.thumbArt(c, it, 34)
						ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(54).
							Gradient(ui.Transparent, ui.RGBA(0, 0, 0, 0.55), 180)
					})
					ui.Column(c).Gap(10).Grow(1).Basis(0).Children(func() {
						if it.Series != "" {
							label := it.Series
							if ep := it.EpisodeLabel(); ep != "" {
								label += " · " + ep
							}
							ui.Text(c, label).FontSize(11).FontWeight(700).TextColor(p.accent)
						}
						ui.Text(c, it.Name).FontSize(30).FontWeight(650).SingleLine()
						ui.Text(c, it.Subtitle()).FontSize(12.5).TextColor(p.muted)
						ui.Row(c).Gap(10).AlignItems(ui.Center).PaddingY(6).Children(func() {
							label := "播放"
							if hasProg && prog.Started() {
								label = "继续观看"
							}
							primaryButton(c, label, iconPlay, func() { a.play(a.detailPlayTarget(it), false) })
							if hasProg && prog.Started() {
								ghostButton(c, "从头播放", nil, func() { a.play(a.detailPlayTarget(it), true) })
							}
							ghostButton(c, "片库", iconLibrary, func() { a.goPage(pageLibrary) })
						})
					})
				})
			})

			eps := a.seriesOf(it)
			if it.Source != "" && it.Source != "webdav" && it.Source != "iptv" && len(a.detailEpisodes) > 0 {
				eps = a.detailEpisodes
			}
			if len(eps) > 1 {
				ui.Column(c).Gap(10).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						ui.Text(c, "选集").FontSize(13).FontWeight(600)
						ui.Text(c, fmt.Sprintf("%d 集", len(eps))).FontSize(11).TextColor(p.faint)
					})
					for i := 0; i < len(eps); i += 6 {
						end := min(i+6, len(eps))
						ui.Row(c).Gap(8).Children(func() {
							for _, ep := range eps[i:end] {
								a.episodeChip(c, ep, ep.ID == it.ID)
							}
						})
					}
				})
			}

			if hasProg && prog.Position > 0 {
				ui.Column(c).Gap(8).Padding(18).Radius(10).Background(p.panel).Border(1, p.hairline).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						ui.Text(c, "观看进度").FontSize(13).FontWeight(600)
						ui.Text(c, media.HumanDuration(prog.Position)+" / "+media.HumanDuration(prog.Duration)).
							FontSize(11.5).TextColor(p.muted)
						ui.Box(c).Grow(1)
						if at := finished(prog.Updated); at != "" {
							ui.Text(c, at).FontSize(11).TextColor(p.faint)
						}
					})
					progressLine(c, float32(prog.Fraction()))
					ui.Row(c).Children(func() {
						ghostButton(c, "清除记录", nil, func() {
							a.store.Forget(it.ID)
							a.invalidate()
						})
					})
				})
			}

			ui.Column(c).Gap(2).Padding(18).Radius(10).Background(p.panel).Border(1, p.hairline).Children(func() {
				label := "文件"
				if it.Source != "" {
					label = "信息"
				}
				ui.Text(c, label).FontSize(13).FontWeight(600).PaddingY(4)
				switch it.Source {
				case "webdav":
					fact(c, "来源", "WebDAV")
					fact(c, "大小", media.HumanSize(it.Size))
					fact(c, "位置", it.RemoteID)
					if !it.ModTime.IsZero() {
						fact(c, "修改", it.ModTime.Format("2006-01-02 15:04"))
					}
				case "":
					fact(c, "格式", it.Ext)
					fact(c, "大小", media.HumanSize(it.Size))
					fact(c, "位置", it.Path)
					fact(c, "修改", it.ModTime.Format("2006-01-02 15:04"))
					if it.Ext != ".mp4" && it.Ext != ".mov" && it.Ext != ".m4v" {
						fact(c, "格式提示", "这个容器依赖系统解码器；如果打不开，可以转成 MP4。")
					}
				default:
					fact(c, "来源", a.sourceName())
					if it.Duration > 0 {
						fact(c, "时长", media.HumanDuration(it.Duration))
					}
					if it.URL != "" {
						fact(c, "页面", it.URL)
					}
					if it.Overview != "" {
						ui.Text(c, it.Overview).FontSize(12).TextColor(p.muted).
							PaddingY(6)
					}
				}
			})
		})
	})
}

func fact(c *ui.Context, key, value string) {
	p := colors()
	ui.Row(c).Gap(14).PaddingY(3).Children(func() {
		ui.Text(c, key).FontSize(11.5).TextColor(p.faint).Width(56)
		ui.Text(c, value).FontSize(11.5).TextColor(p.muted).SingleLine().Grow(1)
	})
}

// progressLine is a thin bar of the part watched.
func progressLine(c *ui.Context, fraction float32) {
	p := colors()
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	ui.Box(c).Height(4).Radius(2).Background(ui.RGBA(255, 255, 255, 0.14)).Children(func() {
		ui.Row(c).Fill().Children(func() {
			ui.Box(c).Grow(fraction).Height(4).Background(p.accent)
			ui.Box(c).Grow(1 - fraction)
		})
	})
}
