package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/ffbin"
	"mygo-video/internal/plugin"
)

// settingsPage holds what the app does with files, and what it will do with
// plugins.
func (a *app) settingsPage(c *ui.Context) {
	p := colors()
	ui.Row(c).Height(56).PaddingX(32).AlignItems(ui.Center).Gap(10).DragWindow().Children(func() {
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

			settingsCard(c, "关于", func() {
				fact(c, "版本", version())
				fact(c, "框架", "MyGo v0.9.0")
				fact(c, "数据", a.dataDir)
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
