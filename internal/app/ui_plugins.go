package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// pluginsPage manages the plugins: which are on, and what they ask for.
func (a *app) pluginsPage(c *ui.Context) {
	p := colors()
	bar := a.dragWindow(headerRow(c, 32))
	bar.Children(func() {
		ui.Text(c, "插件").FontSize(15).FontWeight(600)
		ui.Text(c, fmt.Sprintf("%d 个已启用 · 共 %d 个", a.enabledPlugins(), len(a.plugins))).
			FontSize(12).TextColor(p.faint)
		ui.Box(c).Grow(1)
		a.installMenuButton(c)
	})

	ui.Scroll(c).Grow(1).TrackScroll(&a.pluginsScroll).Children(func() {
		ui.Column(c).Padding(8, 32, 40, 32).Gap(14).Children(func() {
			ui.Text(c, "停用的插件不会加载，也不会出现在侧栏的源列表里；设置、账号和播放记录会保留。").
				FontSize(11.5).TextColor(p.muted)

			a.builtinSources(c)

			ui.Text(c, "插件").FontSize(12).FontWeight(600).TextColor(p.muted).PaddingY(2)
			a.pluginsToolbar(c)

			if len(a.plugins) == 0 {
				emptyState(c, "还没有插件", "可以从文件夹、zip 或 GitHub 安装。装好后在列表里打开开关启用。", nil)
				return
			}
			list := a.pluginsShowing()
			if len(list) == 0 {
				emptyState(c, "没有符合条件的插件", "换个筛选，或清空搜索词。", nil)
				return
			}
			ui.Column(c).Gap(10).Children(func() {
				for _, pe := range list {
					a.pluginCard(c, pe)
				}
			})
			ui.Text(c, "插件代码以你的用户权限运行，安装前请确认来源。MyGo Video 不包含 DRM 破解。").
				FontSize(10.5).TextColor(p.faint)
		})
	})
}

// pluginsToolbar filters the list and searches it.
func (a *app) pluginsToolbar(c *ui.Context) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		a.pluginChip(c, "全部", "")
		a.pluginChip(c, fmt.Sprintf("已启用 %d", a.enabledPlugins()), "on")
		a.pluginChip(c, fmt.Sprintf("未启用 %d", len(a.plugins)-a.enabledPlugins()), "off")
		ui.Box(c).Grow(1)
		ui.TextInput(c, &a.pluginQuery).Placeholder("搜索插件").Height(30).Width(220)
	})
}

func (a *app) pluginChip(c *ui.Context, label, filter string) {
	p := colors()
	active := a.pluginFilter == filter
	b := ui.ButtonBase(c).Height(28).PaddingX(12).Radius(7).Label(label).
		AlignItems(ui.Center).Justify(ui.Center)
	if active {
		b.Background(p.accent)
	} else {
		b.Background(ui.RGBA(255, 255, 255, 0.04))
		if b.Hovered() {
			b.Background(p.hover)
		}
	}
	b.OnClick(func() {
		a.pluginFilter = filter
		a.invalidate()
	})
	b.Children(func() {
		color := p.text
		if active {
			color = ui.Hex("#0d0e11")
		}
		ui.Text(c, label).FontSize(11.5).TextColor(color).SingleLine()
	})
}

// pluginsShowing is the plugins the filter and the search leave.
func (a *app) pluginsShowing() []*pluginEntry {
	q := strings.ToLower(strings.TrimSpace(a.pluginQuery))
	var out []*pluginEntry
	for _, pe := range a.plugins {
		on := pe.p != nil
		switch a.pluginFilter {
		case "on":
			if !on {
				continue
			}
		case "off":
			if on {
				continue
			}
		}
		if q != "" {
			hay := strings.ToLower(pe.man.ID + " " + pe.man.Name + " " + pe.man.Description)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		out = append(out, pe)
	}
	return out
}

// pluginCard is one plugin, open to its account and settings when clicked.
func (a *app) pluginCard(c *ui.Context, pe *pluginEntry) {
	p := colors()
	expanded := a.pluginOpen == pe.man.ID
	border := p.hairline
	switch {
	case expanded:
		border = ui.RGBA(255, 255, 255, 0.16)
	case pe.err != nil:
		border = ui.RGBA(239, 143, 122, 0.32)
	case a.pluginIsNew(pe):
		border = ui.RGBA(231, 162, 94, 0.5)
	}
	card := ui.Column(c).Radius(11).Background(p.panel).Border(1, border)
	card.Children(func() {
		head := ui.Row(c).Gap(14).AlignItems(ui.Center).Padding(14, 16, 14, 16)
		head.OnClick(func() {
			if expanded {
				a.pluginOpen = ""
			} else {
				a.pluginOpen = pe.man.ID
			}
			a.pluginUninstall = ""
			a.invalidate()
		})
		head.Children(func() {
			c1, c2 := pluginTint(pe.man.ID)
			ui.Box(c).Size(46, 46).Radius(11).Gradient(c1, c2, 145).Center().Children(func() {
				ui.Text(c, firstRune(pe.man.Name)).FontSize(15).FontWeight(700).TextColor(ui.Hex("#14100a"))
			})

			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, pe.man.Name).FontSize(13.5).FontWeight(600).SingleLine()
					if pe.man.Version != "" {
						ui.Text(c, "v"+pe.man.Version).FontSize(10.5).TextColor(p.faint)
					}
					a.pluginBadge(c, pe)
				})
				desc := pe.man.Description
				if pe.err != nil {
					desc = pe.err.Error()
				}
				if desc == "" {
					desc = "没有说明。"
				}
				ui.Text(c, desc).FontSize(11.5).TextColor(p.faint).SingleLine()
				ui.Row(c).Gap(6).Children(func() {
					playback := "直链"
					if pe.man.Playback == "catalog" {
						playback = "目录"
					}
					pluginTag(c, playback, p.muted)
					if hs := pe.man.Permissions.Network; len(hs) > 0 {
						label := "网络 " + hs[0]
						if len(hs) > 1 {
							label = fmt.Sprintf("网络 %s 等 %d 个", hs[0], len(hs))
						}
						pluginTag(c, label, p.muted)
					}
					if pe.p != nil && pe.p.Can("signIn") {
						if acc := a.accountFor(pe.p); acc != nil {
							pluginTag(c, "已登录 · "+acc.Name, ui.Hex("#7ec8a0"))
						} else {
							pluginTag(c, "未登录", p.muted)
						}
					}
				})
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if pe.busy {
					ui.Text(c, "启用中…").FontSize(11).TextColor(p.accent)
				} else {
					a.pluginSwitch(c, pe)
				}
			})
		})

		if expanded {
			a.pluginDetail(c, pe)
		}
	})
}

// pluginDetail is what the open card shows below its head.
func (a *app) pluginDetail(c *ui.Context, pe *pluginEntry) {
	p := colors()
	ui.Column(c).Gap(9).Padding(2, 16, 14, 76).Children(func() {
		if pe.man.Login != nil || (pe.p != nil && pe.p.Can("signIn")) {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "账号").FontSize(11).TextColor(p.faint).Width(34)
				a.pluginLoginRow(c, pe)
			})
		}
		a.pluginFields(c, pe)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "信息").FontSize(11).TextColor(p.faint).Width(34)
			ui.Text(c, fmt.Sprintf("%s · API v%d · %s", pe.man.ID, pe.man.APIVersion, pe.dir)).
				FontSize(10.5).TextColor(p.faint).SingleLine()
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "操作").FontSize(11).TextColor(p.faint).Width(34)
			smallButton(c, "打开目录", func() {
				_ = mygo.Shell.OpenExternal("file://" + pe.dir)
			}, false)
			if pe.p != nil {
				smallButton(c, "重新加载", func() {
					pe.p.Close()
					pe.p = nil
					a.forgetPluginAccount(pe.man.ID)
					a.setPluginEnabled(pe, true)
				}, false)
			}
			ui.Box(c).Grow(1)
			confirm := a.pluginUninstall == pe.man.ID
			label := "卸载插件"
			if confirm {
				label = "确认卸载"
			}
			smallButton(c, label, func() {
				if !confirm {
					a.pluginUninstall = pe.man.ID
					a.invalidate()
					return
				}
				a.pluginUninstall = ""
				a.uninstallPlugin(pe)
			}, true)
		})
	})
}

// pluginFields are the settings the plugin declares, as the open card shows
// them.
func (a *app) pluginFields(c *ui.Context, pe *pluginEntry) {
	p := colors()
	for _, st := range pe.man.Settings {
		key := pe.man.ID + "/" + st.Key
		value := a.settingTexts[key]
		if value == nil {
			v := ""
			if pe.p != nil {
				v = pe.p.Setting(st.Key)
			}
			value = &v
			a.settingTexts[key] = value
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "设置").FontSize(11).TextColor(p.faint).Width(34)
			ui.Text(c, st.Label).FontSize(11.5).TextColor(p.muted)
			input := ui.TextInput(c, value).Placeholder(st.Help).Height(28).Width(220)
			if input.Changed() && pe.p != nil {
				_ = pe.p.SetSetting(st.Key, *value)
			}
		})
	}
}

// pluginSwitch turns a plugin on or off.
func (a *app) pluginSwitch(c *ui.Context, pe *pluginEntry) {
	p := colors()
	on := pe.p != nil
	bg := p.accent
	if !on {
		bg = ui.RGBA(255, 255, 255, 0.24)
	}
	left := float32(2)
	if on {
		left = 16
	}
	b := ui.ButtonBase(c).Size(34, 20).Radius(10).Background(bg).Label("启用")
	b.OnClick(func() { a.setPluginEnabled(pe, !on) })
	b.Children(func() {
		ui.Box(c).Size(16, 16).Radius(8).Background(ui.Hex("#ffffff")).Margin(2, 2, 2, left)
	})
}

func (a *app) pluginBadge(c *ui.Context, pe *pluginEntry) {
	p := colors()
	label, color := "已停用", p.faint
	bg := ui.RGBA(255, 255, 255, 0.06)
	switch {
	case pe.busy:
		label, color, bg = "启用中…", p.accent, ui.RGBA(231, 162, 94, 0.14)
	case pe.err != nil:
		label, color, bg = "启用失败", ui.Hex("#ef8f7a"), ui.RGBA(239, 143, 122, 0.14)
	case pe.p != nil:
		label, color, bg = "已启用", ui.Hex("#7ec8a0"), ui.RGBA(126, 200, 160, 0.14)
	case a.pluginIsNew(pe):
		label, color, bg = "新安装", p.accent, ui.RGBA(231, 162, 94, 0.14)
	}
	ui.Text(c, label).FontSize(9.5).FontWeight(700).TextColor(color).Padding(2, 6).Radius(4).Background(bg)
}

// pluginLoginRow is the account of a plugin: who is in, and the way in or
// out.
func (a *app) pluginLoginRow(c *ui.Context, pe *pluginEntry) {
	p := colors()
	if pe.p == nil {
		ui.Text(c, "启用后可登录").FontSize(11.5).TextColor(p.faint)
		return
	}
	if acc := a.accountFor(pe.p); acc != nil {
		ui.Text(c, "已登录："+acc.Name).FontSize(11.5).TextColor(p.muted)
		if pe.man.Login != nil {
			smallButton(c, "重新登录", func() { a.startSignIn(pe) }, false)
		}
		smallButton(c, "退出登录", func() { a.signOutEntry(pe) }, false)
		return
	}
	if a.readPluginCookie(pe.man.ID) != "" {
		ui.Text(c, "已登录").FontSize(11.5).TextColor(p.muted)
		if pe.man.Login != nil {
			smallButton(c, "重新登录", func() { a.startSignIn(pe) }, false)
		}
		smallButton(c, "退出登录", func() { a.signOutEntry(pe) }, false)
		return
	}
	ui.Text(c, "未登录").FontSize(11.5).TextColor(p.faint)
	switch {
	case pe.man.Login != nil:
		smallButton(c, "登录", func() { a.startSignIn(pe) }, false)
	case pe.p.Can("signIn"):
		smallButton(c, "登录", func() { a.signInPlugin(pe.p) }, false)
	}
}

// pluginIsNew says a plugin was installed and never switched on.
func (a *app) pluginIsNew(pe *pluginEntry) bool {
	return pe.state.InstalledAt > 0 && pe.state.EnabledAt == 0 && pe.p == nil
}

func pluginTag(c *ui.Context, label string, color ui.Color) {
	ui.Text(c, label).FontSize(9.5).TextColor(color).Padding(2, 6).Radius(4).
		Background(ui.RGBA(255, 255, 255, 0.06)).SingleLine()
}

func smallButton(c *ui.Context, label string, onClick func(), danger bool) {
	p := colors()
	color := p.text
	if danger {
		color = ui.Hex("#ef8f7a")
	}
	b := ui.ButtonBase(c).Height(26).PaddingX(10).Radius(6).Label(label).
		AlignItems(ui.Center).TextColor(color).Background(ui.RGBA(255, 255, 255, 0.06))
	if b.Hovered() {
		b.Background(ui.RGBA(255, 255, 255, 0.1))
	}
	b.OnClick(onClick)
	b.Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(color)
	})
}

// pluginTint is the color of a plugin's mark, from its id.
func pluginTint(id string) (ui.Color, ui.Color) {
	tints := [][2]string{
		{"#f0b048", "#d2693a"},
		{"#f39ab4", "#c2607e"},
		{"#7fa8dc", "#4a6fa5"},
		{"#b6a6e0", "#7a6bab"},
		{"#9ec6a4", "#5f8f6d"},
		{"#8caad2", "#5a7699"},
	}
	h := uint32(2166136261)
	for i := 0; i < len(id); i++ {
		h ^= uint32(id[i])
		h *= 16777619
	}
	t := tints[int(h%uint32(len(tints)))]
	return ui.Hex(t[0]), ui.Hex(t[1])
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return "?"
}

// builtinSources are the sources that come with the app, managed on this
// page with the plugins.
func (a *app) builtinSources(c *ui.Context) {
	p := colors()
	ui.Text(c, "内置源").FontSize(12).FontWeight(600).TextColor(p.muted).PaddingY(2)
	ui.Column(c).Gap(10).Children(func() {
		a.builtinCard(c, "local", "本地视频", ui.Hex("#8caad2"), ui.Hex("#5a7699"), "本",
			fmt.Sprintf("%d 个视频 · %d 个文件夹", a.lib.Count(), len(a.lib.Roots())), p.faint, func() {
				for _, root := range a.lib.Roots() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconFolder).Size(14, 14).TextColor(p.faint)
						ui.Text(c, root).FontSize(11.5).TextColor(p.muted).SingleLine()
					})
				}
				ui.Row(c).Gap(8).Children(func() {
					smallButton(c, "重新扫描", func() {
						go a.scan()
						a.invalidate()
					}, false)
				})
			})

		webStatus, webColor := "未配置", p.faint
		switch {
		case a.webdavErr != "":
			webStatus, webColor = a.webdavErr, ui.Hex("#ef8f7a")
		case a.webdav != nil:
			webStatus, webColor = fmt.Sprintf("已连接 · %d 个视频", len(a.webdavItems())), ui.Hex("#7ec8a0")
		}
		a.builtinCard(c, "webdav", "WebDAV", ui.Hex("#9ec6a4"), ui.Hex("#5f8f6d"), "W",
			webStatus, webColor, func() {
				settingsField(c, "地址", &a.webdavURL, "https://nas.local:5005/dav")
				settingsField(c, "用户名", &a.webdavUser, "可留空")
				settingsField(c, "密码", &a.webdavPass, "可留空")
				ui.Row(c).Gap(8).Children(func() {
					smallButton(c, "保存", a.saveWebDAV, false)
				})
			})

		iptvColor := p.faint
		if a.iptvErr != "" {
			iptvColor = ui.Hex("#ef8f7a")
		}
		a.builtinCard(c, "iptv", "IPTV", ui.Hex("#f0b048"), ui.Hex("#d2693a"), "I",
			a.iptvLine(), iptvColor, func() {
				settingsField(c, "列表地址", &a.iptvURL, "https://example.com/live.m3u")
				ui.Row(c).Gap(8).Children(func() {
					smallButton(c, "保存", a.saveIPTV, false)
				})
			})
	})
}

// builtinCard is one built-in source, open to its settings when clicked.
func (a *app) builtinCard(c *ui.Context, key, name string, tint1, tint2 ui.Color, mark, status string, statusColor ui.Color, body func()) {
	p := colors()
	open := a.pluginOpen == "builtin:"+key
	ui.Column(c).Radius(11).Background(p.panel).Border(1, p.hairline).Children(func() {
		head := ui.Row(c).Gap(14).AlignItems(ui.Center).Padding(14, 16, 14, 16)
		head.OnClick(func() {
			if open {
				a.pluginOpen = ""
			} else {
				a.pluginOpen = "builtin:" + key
			}
			a.pluginUninstall = ""
			a.invalidate()
		})
		head.Children(func() {
			ui.Box(c).Size(46, 46).Radius(11).Gradient(tint1, tint2, 145).Center().Children(func() {
				ui.Text(c, mark).FontSize(15).FontWeight(700).TextColor(ui.Hex("#14100a"))
			})
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, name).FontSize(13.5).FontWeight(600)
					ui.Text(c, "内置").FontSize(9.5).TextColor(p.faint).Padding(2, 6).Radius(4).
						Background(ui.RGBA(255, 255, 255, 0.06))
				})
				ui.Text(c, status).FontSize(11.5).TextColor(statusColor).SingleLine()
			})
			ui.Icon(c, iconDown).Size(13, 13).TextColor(p.faint)
		})
		if open {
			ui.Column(c).Gap(9).Padding(2, 16, 14, 76).Children(body)
		}
	})
}
