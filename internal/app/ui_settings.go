package app

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/ffbin"
	"mygo-video/internal/plugin"
)

// settingsPage holds what the app does with files, and what it will do with
// plugins.
func (a *app) settingsPage(c *ui.Context) {
	p := colors()
	bar := a.dragWindow(headerRow(c, 32))
	bar.Children(func() {
		ui.Text(c, "设置").FontSize(15).FontWeight(600)
		ui.Box(c).Grow(1)
	})

	ui.Scroll(c).Grow(1).TrackScroll(&a.settingsScroll).Children(func() {
		ui.Column(c).Padding(8, 32, 40, 32).Gap(16).MaxWidth(820).Children(func() {
			settingsCard(c, "工具", func() {
				ui.Text(c, "ffmpeg 用来抽帧、把 WebDAV 的大文件转成 HLS、合成 1080P。没有也能播放本地视频。").
					FontSize(11.5).TextColor(p.faint)
				a.toolRow(c, "ffmpeg", a.ffmpeg)
				ui.Row(c).Gap(8).Children(func() {
					ghostButton(c, "重新检测", iconRefresh, a.detectTools)
				})
			})

			settingsCard(c, "播放", func() {
				fact(c, "空格", "播放 / 暂停")
				fact(c, "← →", "后退 / 快进 10 秒")
				fact(c, "↑ ↓", "音量")
				fact(c, "Esc", "退出播放")
			})

			settingsCard(c, "弹幕", func() {
				a.danmakuPrefs = a.danmakuPrefs.WithDefaults()
				ui.Text(c, "弹幕来自插件，这里调的是显示方式。").
					FontSize(11.5).TextColor(p.faint)
				a.danmakuChoice(c, "字号", []float64{12, 14, 18}, []string{"小", "中", "大"},
					a.danmakuPrefs.Size, func(v float64) { a.danmakuPrefs.Size = v })
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "不透明度").FontSize(11.5).TextColor(p.muted).Width(64)
					op := a.danmakuPrefs.Opacity
					if v := ui.Slider(c, &op, 0.3, 1).Width(180); v.Changed() {
						a.danmakuPrefs.Opacity = op
						a.saveDanmaku()
					}
					ui.Text(c, fmt.Sprintf("%d%%", int(op*100))).FontSize(11).TextColor(p.faint)
				})
				a.danmakuChoice(c, "速度", []float64{12, 9, 6}, []string{"慢", "中", "快"},
					a.danmakuPrefs.Speed, func(v float64) { a.danmakuPrefs.Speed = v })
				a.danmakuChoice(c, "显示区域", []float64{0.33, 0.5, 1}, []string{"三分之一", "半屏", "全屏"},
					a.danmakuPrefs.Area, func(v float64) { a.danmakuPrefs.Area = v })
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, "屏蔽词").FontSize(11.5).TextColor(p.muted)
					if v := ui.TextInput(c, &a.danmakuPrefs.Block).
						Placeholder("用逗号分开，比如 剧透,广告").
						Height(32).MaxWidth(460); v.Changed() {
						a.saveDanmaku()
					}
				})
			})

			settingsCard(c, "关于", func() {
				fact(c, "版本", version())
				fact(c, "框架", "MyGo v0.10.0")
				fact(c, "数据", a.dataDir)
				ui.Row(c).Gap(8).Children(func() {
					ghostButton(c, "更改…", iconFolder, a.changeDataDir)
					ghostButton(c, "打开目录", iconFolder, func() {
						_ = mygo.Shell.OpenExternal("file://" + a.dataDir)
					})
					if a.dataDirCustom {
						ghostButton(c, "恢复默认", nil, a.resetDataDir)
					}
				})
				ui.Text(c, "仅播放你有权访问的内容。").
					FontSize(11).TextColor(p.faint).PaddingY(4)
			})
		})
	})
}

// toolRow is one program the app looks for.
func (a *app) toolRow(c *ui.Context, name, path string) {
	p := colors()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, name).FontSize(11.5).TextColor(p.muted).Width(64)
		if path != "" {
			ui.Text(c, path).FontSize(10.5).TextColor(p.faint).SingleLine().Shrink(1)
		} else {
			ui.Text(c, "未找到").FontSize(11).TextColor(ui.Hex("#ef8f7a"))
		}
		ui.Box(c).Grow(1)
		ghostButton(c, "选择…", iconFolder, func() { a.chooseTool(name) })
	})
}

// chooseTool asks where a program is.
func (a *app) chooseTool(name string) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:       "选择 " + name,
			ButtonLabel: "使用",
			Message:     "选 " + name + " 的位置",
		})
		if err != nil || len(paths) == 0 {
			return
		}
		mygo.RunOnMain(func() {
			a.store.SetTool(name, paths[0])
			a.detectTools()
		})
	}()
}

// detectTools looks for ffmpeg again, and tells the plugins.
func (a *app) detectTools() {
	tools := a.store.Tools()
	a.ffmpeg = ffbin.Locate("ffmpeg", tools.FFmpeg, a.toolBin)
	for _, pe := range a.plugins {
		if pe.p != nil {
			pe.p.SetFFmpeg(a.ffmpeg != "")
		}
	}
	state := "未找到"
	if a.ffmpeg != "" {
		state = a.ffmpeg
	}
	a.say("ffmpeg：%s", state)
	a.invalidate()
}

// danmakuChoice is one segmented row over a numeric setting.
func (a *app) danmakuChoice(c *ui.Context, label string, values []float64, labels []string, current float64, set func(float64)) {
	p := colors()
	idx := 0
	for i, v := range values {
		if v == current {
			idx = i
		}
	}
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(p.muted).Width(64)
		if v := ui.Segmented(c, &idx, labels...); v.Changed() {
			set(values[idx])
			a.saveDanmaku()
		}
	})
}

func settingsCard(c *ui.Context, title string, body func()) {
	p := colors()
	ui.Column(c).Gap(10).Padding(18).Radius(10).
		Background(p.panel).Border(1, p.hairline).Children(func() {
		ui.Text(c, title).FontSize(13.5).FontWeight(600)
		body()
	})
}

func settingsField(c *ui.Context, label string, value *string, placeholder string) {
	p := colors()
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(p.muted)
		ui.TextInput(c, value).Placeholder(placeholder).Height(32).MaxWidth(460)
	})
}

func (a *app) pluginAccount(c *ui.Context, pl *plugin.Plugin) {
	p := colors()
	id := pl.Info().ID
	if a.pluginAccountBusy[id] {
		ui.Text(c, "检查账号…").FontSize(11).TextColor(p.faint)
		return
	}
	if acc := a.accountFor(pl); acc != nil {
		ui.Text(c, "已登录："+acc.Name).FontSize(11).TextColor(p.muted)
		ghostButton(c, "退出", nil, func() { a.signOutPlugin(pl) })
		return
	}
	if pl.Can("signIn") {
		ghostButton(c, "登录", nil, func() { a.signInPlugin(pl) })
	}
}

func version() string {
	if mygo.IsDev() {
		return "开发版"
	}
	if v := mygo.App.Version(); v != "" {
		return v
	}
	return "0.1.0"
}
