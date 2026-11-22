package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/plugin"
)

// officialPluginRepo is where the app publishes its own plugins: a zip for
// each one and plugins.json, beside the installers of the release.
const officialPluginRepo = "pingfury108/mygo-video"

const marketSchema = 1

type marketIndex struct {
	Schema  int            `json:"schema"`
	Plugins []marketPlugin `json:"plugins"`
}

type marketPlugin struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Version       string             `json:"version"`
	APIVersion    int                `json:"apiVersion"`
	MinAppVersion string             `json:"minAppVersion,omitempty"`
	Description   string             `json:"description,omitempty"`
	Playback      string             `json:"playback,omitempty"`
	Permissions   plugin.Permissions `json:"permissions"`
	NeedsLogin    bool               `json:"login,omitempty"`
	Asset         string             `json:"asset"`
	SHA256        string             `json:"sha256,omitempty"`
}

func marketIndexURL() string {
	return "https://github.com/" + officialPluginRepo + "/releases/latest/download/plugins.json"
}

func marketAssetURL(asset string) string {
	return "https://github.com/" + officialPluginRepo + "/releases/latest/download/" + url.PathEscape(asset)
}

func parseMarket(data []byte) ([]marketPlugin, error) {
	var idx marketIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("插件市场的数据读不懂：%w", err)
	}
	if idx.Schema != marketSchema {
		return nil, fmt.Errorf("插件市场的数据版本 %d 不认识", idx.Schema)
	}
	out := idx.Plugins[:0]
	for _, p := range idx.Plugins {
		if p.ID == "" || p.Name == "" || p.Asset == "" {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// openMarket shows the official plugins and reads the list once.
func (a *app) openMarket() {
	a.installMenu = false
	a.marketDialog = true
	a.marketErr = ""
	a.invalidate()
	a.loadMarket(false)
}

// loadMarket reads the list the latest release publishes.
func (a *app) loadMarket(force bool) {
	if a.marketLoading || (!force && a.marketFetched) {
		return
	}
	a.marketLoading = true
	a.marketErr = ""
	a.invalidate()
	go func() {
		client := &http.Client{Timeout: 30 * time.Second}
		data, err := getBytes(client, marketIndexURL(), false)
		var plugins []marketPlugin
		if err == nil {
			plugins, err = parseMarket(data)
		}
		mygo.RunOnMain(func() {
			a.marketLoading = false
			if err != nil {
				a.marketErr = err.Error()
				a.invalidate()
				return
			}
			a.marketPlugins = plugins
			a.marketFetched = true
			a.invalidate()
		})
	}()
}

// installMarket downloads one plugin and installs it, replacing an older
// copy of the same id while keeping its data and enabled state.
func (a *app) installMarket(m marketPlugin) {
	if a.marketInstalling != "" {
		return
	}
	if !marketFits(m) {
		a.marketErr = m.Name + " 需要更新的 MyGo Video"
		a.invalidate()
		return
	}
	a.marketInstalling = m.ID
	a.marketErr = ""
	a.invalidate()
	go func() {
		client := &http.Client{Timeout: 2 * time.Minute}
		data, err := getBytes(client, marketAssetURL(m.Asset), false)
		if err == nil {
			err = verifyMarketZip(data, m.SHA256)
		}
		var src string
		if err == nil {
			var dir string
			dir, err = os.MkdirTemp("", "mygo-video-plugin-")
			if err == nil {
				defer os.RemoveAll(dir)
				if err = unzip(data, dir); err == nil {
					src, err = findPluginRoot(dir)
				}
			}
		}
		mygo.RunOnMain(func() {
			a.marketInstalling = ""
			if err != nil {
				a.marketErr = err.Error()
				a.invalidate()
				return
			}
			name, err := a.installDirAt(src, "market", true)
			if err != nil {
				a.marketErr = err.Error()
				a.invalidate()
				return
			}
			a.say("已安装 %s，打开开关后启用", name)
			a.invalidate()
		})
	}()
}

func verifyMarketZip(data []byte, want string) error {
	if want == "" {
		return nil
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), want) {
		return errors.New("插件压缩包校验失败，请稍后重试")
	}
	return nil
}

// marketFits says the app speaks the plugin's API and is new enough.
func marketFits(m marketPlugin) bool {
	if m.APIVersion > plugin.APIVersion {
		return false
	}
	if m.MinAppVersion != "" && plugin.CompareVersion(mygo.App.Version(), m.MinAppVersion) < 0 {
		return false
	}
	return true
}

func (a *app) marketInstalled(m marketPlugin) string {
	pe := a.pluginEntryByID(m.ID)
	if pe == nil {
		return ""
	}
	return pe.man.Version
}

func (a *app) marketDialogView(c *ui.Context) {
	if !a.marketDialog {
		return
	}
	p := colors()
	ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).
		Background(ui.RGBA(0, 0, 0, 0.55)).Center().Children(func() {
		ui.Column(c).Width(640).MaxWidthPercent(92).HeightPercent(86).MaxHeight(680).
			Padding(22).Gap(12).Radius(14).Background(p.panel).Border(1, p.hairline).
			Shadow(0, 20, 48, 4, p.cardShade).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconPlugin).Size(18, 18).TextColor(p.accent)
				ui.Column(c).Gap(2).Grow(1).Children(func() {
					ui.Text(c, "插件市场").FontSize(16).FontWeight(600)
					ui.Text(c, "随应用版本发布的官方插件").FontSize(11).TextColor(p.muted)
				})
				iconButton(c, iconRefresh, 14, "刷新", func() { a.loadMarket(true) })
			})

			if a.marketErr != "" {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.marketErr).FontSize(11).TextColor(ui.Hex("#ef8f7a")).Grow(1)
					if !a.marketLoading {
						ghostButton(c, "重试", iconRefresh, func() { a.loadMarket(true) })
					}
				})
			}
			switch {
			case a.marketLoading && len(a.marketPlugins) == 0:
				emptyState(c, "正在读取插件市场…", "从最新的 GitHub Release 获取插件清单。", nil)
			case len(a.marketPlugins) == 0 && a.marketErr == "":
				emptyState(c, "市场里还没有插件", "等下一个版本发布后再来看看。", nil)
			default:
				ui.Scroll(c).Grow(1).TrackScroll(&a.marketScroll).Children(func() {
					ui.Column(c).Gap(10).Children(func() {
						for _, m := range a.marketPlugins {
							a.marketCard(c, m)
						}
					})
				})
			}

			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "只列出本仓库发布的官方插件；安装时会校验 ID、版本和压缩包 SHA-256。").
					FontSize(10.5).TextColor(p.faint).Grow(1)
				ghostButton(c, "关闭", nil, func() { a.marketDialog = false })
			})
		})
	})
}

func (a *app) marketCard(c *ui.Context, m marketPlugin) {
	p := colors()
	ui.Column(c).Gap(7).Padding(14).Radius(10).Background(p.bg).Border(1, p.hairline).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, m.Name).FontSize(13.5).FontWeight(600).SingleLine()
			if m.Version != "" {
				ui.Text(c, "v"+m.Version).FontSize(10.5).TextColor(p.faint)
			}
			if m.NeedsLogin {
				pluginTag(c, "需要登录", p.muted)
			}
			ui.Box(c).Grow(1)
			a.marketAction(c, m)
		})
		if m.Description != "" {
			ui.Text(c, m.Description).FontSize(11.5).TextColor(p.muted)
		}
		ui.Row(c).Gap(6).Children(func() {
			playback := "直链"
			if m.Playback == "catalog" {
				playback = "目录"
			}
			pluginTag(c, playback, p.muted)
			if hs := m.Permissions.Network; len(hs) > 0 {
				label := "网络 " + hs[0]
				if len(hs) > 1 {
					label = fmt.Sprintf("网络 %s 等 %d 个", hs[0], len(hs))
				}
				pluginTag(c, label, p.muted)
			}
			if m.MinAppVersion != "" {
				pluginTag(c, "应用 "+m.MinAppVersion+" 起", p.faint)
			}
		})
	})
}

func (a *app) marketAction(c *ui.Context, m marketPlugin) {
	p := colors()
	if a.marketInstalling == m.ID {
		ui.Text(c, "正在下载…").FontSize(11).TextColor(p.accent)
		return
	}
	if !marketFits(m) {
		ui.Text(c, "需要新版应用").FontSize(11).TextColor(ui.Hex("#ef8f7a"))
		return
	}
	installed := a.marketInstalled(m)
	switch {
	case installed == "":
		smallButton(c, "安装", func() { a.installMarket(m) }, false)
	case installed == m.Version || plugin.CompareVersion(installed, m.Version) >= 0:
		ui.Text(c, "已安装").FontSize(11).TextColor(ui.Hex("#7ec8a0"))
	default:
		smallButton(c, "更新", func() { a.installMarket(m) }, false)
	}
}
