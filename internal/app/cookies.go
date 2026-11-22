package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// pluginCookieFile is what a signed-in plugin keeps: the Cookie header the
// app read from its login page.
type pluginCookieFile struct {
	Header  string `json:"header"`
	SavedAt int64  `json:"savedAt,omitempty"`
}

func (a *app) pluginCookiePath(id string) string {
	return filepath.Join(a.pluginDataDir, id, "cookies.json")
}

// loadPluginCookies gives each running plugin the cookies kept for it.
func (a *app) loadPluginCookies() {
	for _, pe := range a.plugins {
		if pe.p == nil {
			continue
		}
		if h := a.readPluginCookie(pe.man.ID); h != "" {
			pe.p.SetCookie(h)
		}
	}
}

func (a *app) readPluginCookie(id string) string {
	b, err := os.ReadFile(a.pluginCookiePath(id))
	if err != nil {
		return ""
	}
	var f pluginCookieFile
	if json.Unmarshal(b, &f) != nil {
		return ""
	}
	return f.Header
}

func (a *app) savePluginCookie(id, header string) error {
	if err := os.MkdirAll(filepath.Dir(a.pluginCookiePath(id)), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(pluginCookieFile{Header: header, SavedAt: time.Now().Unix()})
	if err != nil {
		return err
	}
	return os.WriteFile(a.pluginCookiePath(id), b, 0o600)
}

func (a *app) clearPluginCookie(id string) {
	_ = os.Remove(a.pluginCookiePath(id))
}

// startSignIn opens the plugin's login page in a web view over the app.
func (a *app) startSignIn(pe *pluginEntry) {
	if pe.man.Login == nil || pe.man.Login.URL == "" || a.win == nil {
		a.say("这个插件没有登录页")
		return
	}
	v, err := a.win.NewWebView(mygo.WebViewOptions{URL: pe.man.Login.URL})
	if err != nil {
		a.say("%v", err)
		return
	}
	a.signInEntry = pe
	a.signInWeb = v
	a.signInErr = ""
	a.route = pageSignIn
	a.invalidate()
}

// finishSignIn keeps the cookies of the hosts the plugin asked for.
func (a *app) finishSignIn() {
	pe := a.signInEntry
	if pe == nil || pe.man.Login == nil {
		return
	}
	hosts := pe.man.Login.CookieHosts
	cookies, err := mygo.Cookies.List(mygo.CookieFilter{})
	if err != nil {
		a.signInErr = "读不到登录信息：" + err.Error()
		a.invalidate()
		return
	}
	var parts []string
	for _, c := range cookies {
		if !cookieHostMatches(c.Domain, hosts) {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	if len(parts) == 0 {
		a.signInErr = "还没有读到登录信息，先在网页里登录"
		a.invalidate()
		return
	}
	header := strings.Join(parts, "; ")
	if err := a.savePluginCookie(pe.man.ID, header); err != nil {
		a.signInErr = "保存登录信息失败：" + err.Error()
		a.invalidate()
		return
	}
	if pe.p != nil {
		pe.p.SetCookie(header)
	}
	id := pe.man.ID
	a.closeSignIn()
	a.forgetPluginAccount(id)
	a.route = pagePlugins
	a.say("已登录 %s", pe.man.Name)
	a.invalidate()
}

func (a *app) closeSignIn() {
	if a.signInWeb != nil {
		a.signInWeb.Destroy()
	}
	a.signInWeb = nil
	a.signInEntry = nil
	a.signInErr = ""
}

func (a *app) cancelSignIn() {
	a.closeSignIn()
	a.route = pagePlugins
	a.invalidate()
}

// signOutEntry forgets a plugin's cookies and tells it.
func (a *app) signOutEntry(pe *pluginEntry) {
	a.clearPluginCookie(pe.man.ID)
	if pe.p != nil {
		pe.p.SetCookie("")
		if pe.p.Can("signOut") {
			if err := pe.p.SignOut(); err != nil {
				a.say("%v", err)
			}
		}
	}
	a.forgetPluginAccount(pe.man.ID)
	a.invalidate()
}

// cookieHostMatches says a cookie of domain goes to one of the hosts.
func cookieHostMatches(domain string, hosts []string) bool {
	d := strings.TrimPrefix(domain, ".")
	for _, h := range hosts {
		h = strings.TrimPrefix(h, ".")
		if h == "*" || d == h || strings.HasSuffix(d, "."+h) {
			return true
		}
	}
	return false
}
