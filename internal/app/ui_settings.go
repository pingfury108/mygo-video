package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

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
