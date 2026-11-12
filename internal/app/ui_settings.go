package app

import (
	"fmt"

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
			settingsCard(c, "媒体库", func() {
				ui.Text(c, "扫描这些文件夹里的视频文件：").
					FontSize(12).TextColor(p.muted)
				for _, root := range a.lib.Roots() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconFolder).Size(14, 14).TextColor(p.faint)
						ui.Text(c, root).FontSize(12).TextColor(p.muted).SingleLine()
					})
				}
				ui.Row(c).Gap(12).AlignItems(ui.Center).PaddingY(4).Children(func() {
					ghostButton(c, "重新扫描", iconRefresh, func() {
						go a.scan()
						a.invalidate()
					})
					ui.Text(c, fmt.Sprintf("%d 个视频 · %d 个文件夹", a.lib.Count(), len(a.lib.Roots()))).
						FontSize(11).TextColor(p.faint)
				})
			})

			settingsCard(c, "插件", func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Text(c, "插件把更多来源接进来：本地网络、网盘，以及视频站点。").
						FontSize(12).TextColor(p.muted)
					ui.Box(c).Grow(1)
					a.installMenuButton(c)
				})
				if len(a.plugins) == 0 {
					ui.Text(c, "还没有插件。示例在仓库的 examples/demo-plugin。").
						FontSize(11.5).TextColor(p.faint)
					ui.Text(c, "插件目录："+a.pluginsDir).
						FontSize(11).TextColor(p.faint)
					return
				}
				for _, pl := range a.plugins {
					a.pluginSettings(c, pl)
				}
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

func settingsCard(c *ui.Context, title string, body func()) {
	p := colors()
	ui.Column(c).Gap(10).Padding(18).Radius(10).
		Background(p.panel).Border(1, p.hairline).Children(func() {
		ui.Text(c, title).FontSize(13.5).FontWeight(600)
		body()
	})
}

// pluginSettings is one plugin: who is signed in, and what it asks for.
func (a *app) pluginSettings(c *ui.Context, pl *plugin.Plugin) {
	p := colors()
	info := pl.Info()
	ui.Column(c).Gap(8).Padding(12).Radius(9).
		Background(p.bg).Border(1, p.hairline).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, info.Name).FontSize(12.5).FontWeight(600)
			if info.Version != "" {
				ui.Text(c, "v"+info.Version).FontSize(10.5).TextColor(p.faint)
			}
			ui.Box(c).Grow(1)
			a.pluginAccount(c, pl)
		})
		for _, st := range info.Settings {
			key := info.ID + "/" + st.Key
			value := a.settingTexts[key]
			if value == nil {
				v := pl.Setting(st.Key)
				value = &v
				a.settingTexts[key] = value
			}
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, st.Label).FontSize(11.5).TextColor(p.muted)
				input := ui.TextInput(c, value).Placeholder(st.Help).Height(32).MaxWidth(460)
				if input.Changed() {
					_ = pl.SetSetting(st.Key, *value)
				}
			})
		}
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
