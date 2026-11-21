package app

import "github.com/egoist/mygo/ui"

// signInPage shows the plugin's own login page, with the app's way to
// finish once the listener signed in there.
func (a *app) signInPage(c *ui.Context) {
	p := colors()
	title := "登录"
	if a.signInEntry != nil {
		title = "登录 " + a.signInEntry.man.Name
	}
	bar := a.dragWindow(headerRow(c, 20))
	bar.Children(func() {
		ui.Text(c, title).FontSize(15).FontWeight(600)
		ui.Text(c, "登录页由这个插件提供，MyGo Video 不看你的密码").FontSize(11.5).TextColor(p.faint)
		ui.Box(c).Grow(1)
		ghostButton(c, "取消", nil, a.cancelSignIn)
	})

	if a.signInWeb != nil {
		ui.WebView(c, a.signInWeb).Grow(1)
	} else {
		ui.Column(c).Grow(1).Children(func() {
			emptyState(c, "没有登录页", "这个插件没有提供登录地址。", nil)
		})
	}

	ui.Row(c).Height(60).PaddingX(20).AlignItems(ui.Center).Gap(12).
		Background(p.panel).Border(1, p.hairline).Children(func() {
		text := "Cookie 只保存在本机，仅用于这个插件。"
		color := p.faint
		if a.signInErr != "" {
			text = a.signInErr
			color = ui.Hex("#ef8f7a")
		}
		ui.Text(c, text).FontSize(11.5).TextColor(color).SingleLine().Shrink(1)
		ui.Box(c).Grow(1)
		primaryButton(c, "我已完成登录", iconCheck, a.finishSignIn)
	})
}
