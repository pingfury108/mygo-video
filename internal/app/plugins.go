package app

import (
	"io"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"mygo-video/internal/plugin"
)

// installPlugin asks for a folder and installs it into the plugins
// directory, then lists it.
func (a *app) installPlugin() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:       "安装插件",
			ButtonLabel: "安装",
			Message:     "选择一个含 plugin.json 的文件夹",
			Directory:   true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		mygo.RunOnMain(func() { a.installFrom(paths[0]) })
	}()
}

func (a *app) installFrom(src string) {
	p, err := plugin.Load(src, a.pluginDataDir)
	if err != nil {
		a.say("这不是一个插件：%v", err)
		return
	}
	name, id := p.Info().Name, p.Info().ID
	p.Close()
	dst := filepath.Join(a.pluginsDir, id)
	if _, err := os.Stat(dst); err == nil {
		a.say("已经装了 %s", name)
		return
	}
	if err := copyDir(src, dst); err != nil {
		a.say("安装失败：%v", err)
		return
	}
	a.reloadPlugins()
	a.say("已安装 %s", name)
}

// reloadPlugins lists the plugins again after one is installed.
func (a *app) reloadPlugins() {
	a.plugins = plugin.Discover(a.pluginsDir, a.pluginDataDir)
	a.source = 0
	a.pluginBrowse = nil
	a.pluginItems = nil
	a.pluginSearch = nil
	a.pluginErr = ""
	a.searchQuery = ""
	a.settingTexts = map[string]*string{}
	a.pluginAccounts = map[string]*plugin.Account{}
	a.pluginAccountBusy = map[string]bool{}
	a.pluginChecked = map[string]bool{}
	a.sourceMenu = false
	a.invalidate()
}

// copyDir copies a plugin's folder, leaving out its git history and
// anything that isn't a file or a folder.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if d.Name() == ".git" && path != src {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

// accountFor asks a plugin who is signed in, once, for the settings page.
func (a *app) accountFor(p *plugin.Plugin) *plugin.Account {
	id := p.Info().ID
	if a.pluginChecked[id] {
		return a.pluginAccounts[id]
	}
	if a.pluginAccountBusy[id] {
		return nil
	}
	a.pluginAccountBusy[id] = true
	go func() {
		acc, err := p.Account()
		mygo.RunOnMain(func() {
			delete(a.pluginAccountBusy, id)
			a.pluginChecked[id] = true
			if err == nil {
				a.pluginAccounts[id] = acc
			}
			a.invalidate()
		})
	}()
	return nil
}

func (a *app) signInPlugin(p *plugin.Plugin) {
	id := p.Info().ID
	if a.pluginAccountBusy[id] {
		return
	}
	a.pluginAccountBusy[id] = true
	go func() {
		acc, err := p.SignIn()
		mygo.RunOnMain(func() {
			delete(a.pluginAccountBusy, id)
			a.pluginChecked[id] = true
			if err != nil {
				a.say("%v", err)
				return
			}
			a.pluginAccounts[id] = acc
			a.invalidate()
		})
	}()
}

func (a *app) signOutPlugin(p *plugin.Plugin) {
	id := p.Info().ID
	go func() {
		_ = p.SignOut()
		mygo.RunOnMain(func() {
			delete(a.pluginAccounts, id)
			a.pluginChecked[id] = true
			a.invalidate()
		})
	}()
}
