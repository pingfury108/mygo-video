package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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
					ui.Text(c, a.libraryLine()).FontSize(11).TextColor(p.faint)
				})
			})

			settingsCard(c, "插件", func() {
				ui.Text(c, "插件把更多来源接进来：本地网络、网盘，以及视频站点。").
					FontSize(12).TextColor(p.muted)
				ui.Text(c, "L1 直链：插件给出可播放的流，经本机代理注入请求头后播放。").
					FontSize(11.5).TextColor(p.faint)
				ui.Text(c, "L2 目录：插件提供搜索、详情与追剧；受 DRM 保护的正片在官方 App 中播放。").
					FontSize(11.5).TextColor(p.faint)
				ui.Row(c).Children(func() {
					ui.Text(c, "插件系统在下一个版本到来。").
						FontSize(11).TextColor(p.faint).PaddingY(6)
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

func settingsCard(c *ui.Context, title string, body func()) {
	p := colors()
	ui.Column(c).Gap(10).Padding(18).Radius(10).
		Background(p.panel).Border(1, p.hairline).Children(func() {
		ui.Text(c, title).FontSize(13.5).FontWeight(600)
		body()
	})
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
