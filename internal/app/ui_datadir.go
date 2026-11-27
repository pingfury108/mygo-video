package app

import "github.com/egoist/mygo/ui"

// dataDirDialogView blocks the window while its data directory is copied and
// the app is about to restart.
func (a *app) dataDirDialogView(c *ui.Context) {
	if !a.dataDirBusy {
		return
	}
	p := colors()
	status := a.dataDirStatus
	if status == "" {
		status = "正在处理数据目录…"
	}
	ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).
		Background(ui.RGBA(0, 0, 0, 0.68)).OnClick(func() {}).
		Center().Children(func() {
		ui.Column(c).Width(500).MaxWidthPercent(90).Padding(24).Gap(10).
			Radius(14).Background(p.panel).Border(1, p.hairline).
			Shadow(0, 20, 48, 4, p.cardShade).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconFolder).Size(18, 18).TextColor(p.accent)
				ui.Text(c, "正在切换数据目录").FontSize(16).FontWeight(600)
			})
			ui.Text(c, status).FontSize(12.5).TextColor(p.text)
			ui.Text(c, "复制、校验和切换完成前不会播放任何视频，也不能退出应用。").
				FontSize(11.5).TextColor(p.muted)
			ui.Text(c, "完成后应用会自动重启。").
				FontSize(11).TextColor(p.faint)
		})
	})
}
