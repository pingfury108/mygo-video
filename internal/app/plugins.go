package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/plugin"
	"mygo-video/internal/store"
)

// pluginEntry is a plugin on disk, and its code once it is on.
type pluginEntry struct {
	dir   string
	man   plugin.Manifest
	p     *plugin.Plugin
	err   error
	busy  bool
	state store.PluginState
}

// loadPlugins lists the plugins on disk and starts the ones kept on.
func (a *app) loadPlugins() {
	states := a.store.Plugins()
	found := plugin.Scan(a.pluginsDir)
	sort.Slice(found, func(i, j int) bool {
		return strings.ToLower(found[i].Manifest.Name) < strings.ToLower(found[j].Manifest.Name)
	})
	var out []*pluginEntry
	for _, e := range found {
		pe := &pluginEntry{dir: e.Dir, man: e.Manifest, err: e.Err, state: states[e.Manifest.ID]}
		if pe.err == nil && pe.state.Enabled {
			p, err := plugin.Load(pe.dir, a.pluginDataDir)
			if err != nil {
				pe.err = err
			} else {
				pe.p = p
			}
		}
		out = append(out, pe)
	}
	a.plugins = out
}

// sourceID names the source showing, whatever the plugin order.
func (a *app) sourceID() string {
	switch a.source {
	case sourceLocal:
		return "local"
	case sourceWebDAV:
		return "webdav"
	case sourceIPTV:
		return "iptv"
	}
	if i := a.source - sourcePluginBase; i >= 0 && i < len(a.plugins) {
		return "plugin:" + a.plugins[i].man.ID
	}
	return "local"
}

// sourceIndex is where id sits now, or the library when it is gone.
func (a *app) sourceIndex(id string) int {
	switch id {
	case "webdav":
		return sourceWebDAV
	case "iptv":
		return sourceIPTV
	}
	if pid, ok := strings.CutPrefix(id, "plugin:"); ok {
		for i, pe := range a.plugins {
			if pe.man.ID == pid && pe.p != nil {
				return i + sourcePluginBase
			}
		}
	}
	return sourceLocal
}

// reshowSource shows id again, and falls back to the library when it is
// no longer there.
func (a *app) reshowSource(id string) {
	a.source = a.sourceIndex(id)
	a.store.SetSource(a.sourceID())
	a.pluginBrowse = nil
	a.pluginItems = nil
	a.pluginSearch = nil
	a.pluginErr = ""
	a.pluginBusy = false
	a.pluginTab = ""
	a.searchQuery = ""
	a.sourceMenu = false
	a.invalidate()
}

// setPluginEnabled starts or stops a plugin's code.
func (a *app) setPluginEnabled(pe *pluginEntry, on bool) {
	if pe.busy {
		return
	}
	id := pe.man.ID
	if !on {
		if pe.p != nil {
			pe.p.Close()
			pe.p = nil
		}
		a.store.SetPluginEnabled(id, false)
		was := a.sourceID()
		a.source = a.sourceIndex(was)
		a.store.SetSource(a.sourceID())
		a.pluginBrowse = nil
		a.pluginItems = nil
		a.pluginSearch = nil
		a.pluginErr = ""
		a.pluginBusy = false
		a.sourceMenu = false
		a.invalidate()
		return
	}
	if pe.p != nil {
		return
	}
	pe.busy = true
	pe.err = nil
	dir, dataDir := pe.dir, a.pluginDataDir
	go func() {
		p, err := plugin.Load(dir, dataDir)
		mygo.RunOnMain(func() {
			pe.busy = false
			if err != nil {
				pe.err = err
				a.say("启用失败：%v", err)
				a.invalidate()
				return
			}
			pe.p = p
			pe.err = nil
			if h := a.readPluginCookie(id); h != "" {
				pe.p.SetCookie(h)
			}
			a.store.SetPluginEnabled(id, true)
			a.invalidate()
			a.say("已启用 %s", pe.man.Name)
		})
	}()
	a.invalidate()
}

// enabledPlugins is how many are on.
func (a *app) enabledPlugins() int {
	n := 0
	for _, pe := range a.plugins {
		if pe.p != nil {
			n++
		}
	}
	return n
}

// uninstallPlugin closes a plugin and removes it from disk.
func (a *app) uninstallPlugin(pe *pluginEntry) {
	was := a.sourceID()
	id := pe.man.ID
	if pe.p != nil {
		pe.p.Close()
		pe.p = nil
	}
	if err := os.RemoveAll(pe.dir); err != nil {
		a.say("删不掉 %s：%v", pe.man.Name, err)
		return
	}
	a.store.ForgetPlugin(id)
	if a.pluginOpen == id {
		a.pluginOpen = ""
	}
	a.loadPlugins()
	a.reshowSource(was)
	a.say("已卸载 %s", pe.man.Name)
}

// installMenuButton opens the ways to install a plugin.
func (a *app) installMenuButton(c *ui.Context) {
	p := colors()
	anchor := ghostButton(c, "安装插件…", iconFolder, func() {
		a.installMenu = !a.installMenu
	})
	ui.PopoverBase(c, anchor, &a.installMenu, func(panel ui.Element) {
		panel.MinWidth(190).Margin(6, 0, 6, 0).Padding(6).Gap(2).Radius(10).
			Background(p.panel).Border(1, p.hairline).
			Shadow(0, 10, 28, 2, p.cardShade)
		a.installMenuItem(c, "从文件夹…", iconFolder, func() {
			a.installMenu = false
			a.installFolder()
		})
		a.installMenuItem(c, "从 zip…", iconArchive, func() {
			a.installMenu = false
			a.installZipFile()
		})
		a.installMenuItem(c, "从 GitHub…", iconGlobe, func() {
			a.installMenu = false
			a.installDialog = true
			a.installFocus = true
			a.installURL = ""
			a.installErr = ""
		})
	})
}

func (a *app) installMenuItem(c *ui.Context, label string, icon *ui.SVG, onClick func()) {
	p := colors()
	b := ui.ButtonBase(c).Height(30).PaddingX(8).Gap(8).Radius(7).
		AlignItems(ui.Center).Justify(ui.Start).Label(label)
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.OnClick(onClick)
	b.Children(func() {
		ui.Icon(c, icon).Size(13, 13).TextColor(p.muted)
		ui.Text(c, label).FontSize(12.5).TextColor(p.text)
	})
}

// installFolder asks for a folder of a plugin.
func (a *app) installFolder() {
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

// installZipFile asks for a zip of a plugin.
func (a *app) installZipFile() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:       "安装插件",
			ButtonLabel: "安装",
			Message:     "选择一个插件的 zip",
			Filters:     []mygo.FileFilter{{Name: "Zip", Extensions: []string{"zip"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		data, err := os.ReadFile(paths[0])
		if err != nil {
			mygo.RunOnMain(func() { a.say("%v", err) })
			return
		}
		mygo.RunOnMain(func() { a.installZipFrom(data, filepath.Base(paths[0])) })
	}()
}

// installZipFrom unzips a plugin off the main thread, then installs the
// folder it holds.
func (a *app) installZipFrom(data []byte, label string) {
	a.installBusy = true
	a.installErr = ""
	a.invalidate()
	go func() {
		dir, err := os.MkdirTemp("", "mygo-video-plugin-")
		if err != nil {
			mygo.RunOnMain(func() { a.installFailed(err) })
			return
		}
		defer os.RemoveAll(dir)
		if err := unzip(data, dir); err != nil {
			mygo.RunOnMain(func() { a.installFailed(err) })
			return
		}
		src, err := findPluginRoot(dir)
		if err != nil {
			mygo.RunOnMain(func() { a.installFailed(err) })
			return
		}
		mygo.RunOnMain(func() {
			name, err := a.installDir(src, "zip")
			a.installBusy = false
			if err != nil {
				a.installErr = err.Error()
				a.invalidate()
				return
			}
			a.installDialog = false
			a.say("已安装 %s（%s）", name, label)
			a.goPage(pagePlugins)
		})
	}()
}

// installGitHub downloads a plugin's latest release, or its default branch,
// from GitHub and installs it.
func (a *app) installGitHub() {
	owner, repo, err := parseRepo(a.installURL)
	if err != nil {
		a.installErr = err.Error()
		a.invalidate()
		return
	}
	a.installBusy = true
	a.installErr = ""
	a.invalidate()
	go func() {
		data, err := downloadPluginZip(owner, repo)
		if err != nil {
			mygo.RunOnMain(func() { a.installFailed(err) })
			return
		}
		dir, err := os.MkdirTemp("", "mygo-video-plugin-")
		if err != nil {
			mygo.RunOnMain(func() { a.installFailed(err) })
			return
		}
		defer os.RemoveAll(dir)
		if err := unzip(data, dir); err != nil {
			mygo.RunOnMain(func() { a.installFailed(err) })
			return
		}
		src, err := findPluginRoot(dir)
		if err != nil {
			mygo.RunOnMain(func() { a.installFailed(err) })
			return
		}
		mygo.RunOnMain(func() {
			name, err := a.installDir(src, "github")
			a.installBusy = false
			if err != nil {
				a.installErr = err.Error()
				a.invalidate()
				return
			}
			a.installDialog = false
			a.say("已安装 %s（%s/%s）", name, owner, repo)
			a.goPage(pagePlugins)
		})
	}()
}

func (a *app) installFailed(err error) {
	a.installBusy = false
	a.installErr = err.Error()
	a.invalidate()
}

// installDir copies a plugin folder, if it loads, into the plugins
// directory, and lists the sources again.
func (a *app) installDir(src, origin string) (string, error) {
	p, err := plugin.Load(src, a.pluginDataDir)
	if err != nil {
		return "", fmt.Errorf("这不是一个插件：%w", err)
	}
	name, id := p.Info().Name, p.Info().ID
	p.Close()
	dst := filepath.Join(a.pluginsDir, id)
	if _, err := os.Stat(dst); err == nil {
		return "", fmt.Errorf("已经装了 %s", name)
	}
	if err := copyDir(src, dst); err != nil {
		return "", err
	}
	a.store.SetPluginInstalled(id, origin)
	a.pluginOpen = id
	a.reloadPlugins()
	return name, nil
}

func (a *app) installFrom(src string) {
	name, err := a.installDir(src, "folder")
	if err != nil {
		a.say("%v", err)
		return
	}
	a.say("已安装 %s，打开开关后启用", name)
	a.goPage(pagePlugins)
}

// reloadPlugins lists the plugins again after one is installed.
func (a *app) reloadPlugins() {
	was := a.sourceID()
	a.loadPlugins()
	a.reshowSource(was)
	a.settingTexts = map[string]*string{}
	a.pluginAccounts = map[string]*plugin.Account{}
	a.pluginAccountBusy = map[string]bool{}
	a.pluginChecked = map[string]bool{}
	a.installErr = ""
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

// unzip writes a zip into dst, turning away what goes outside it, and what
// is too big to be a plugin.
func unzip(data []byte, dst string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if len(zr.File) > 2000 {
		return errors.New("zip 里的文件太多了")
	}
	var total int64
	for _, f := range zr.File {
		if !filepath.IsLocal(f.Name) {
			return fmt.Errorf("zip 里有不安全的路径：%s", f.Name)
		}
		target := filepath.Join(dst, f.Name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > 100<<20 {
			return errors.New("zip 解压后太大了")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// findPluginRoot is the folder holding plugin.json in an unpacked zip, at
// most a few levels down.
func findPluginRoot(dir string) (string, error) {
	found := ""
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != dir {
			if rel, err := filepath.Rel(dir, path); err == nil && strings.Count(rel, string(os.PathSeparator)) >= 3 {
				return filepath.SkipDir
			}
		}
		if _, err := os.Stat(filepath.Join(path, "plugin.json")); err == nil && found == "" {
			found = path
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", errors.New("zip 里没有 plugin.json")
	}
	return found, nil
}

// parseRepo reads owner and repo from a GitHub address or owner/repo.
func parseRepo(raw string) (string, string, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "www.")
	s = strings.TrimPrefix(s, "github.com/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.Trim(s, "/")
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("需要 owner/repo 或 GitHub 地址")
	}
	return parts[0], parts[1], nil
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// downloadPluginZip is a plugin's latest release zip, or the zip of its
// default branch when it has none.
func downloadPluginZip(owner, repo string) ([]byte, error) {
	client := &http.Client{Timeout: 90 * time.Second}
	release := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	if body, err := getBytes(client, release, true); err == nil {
		var rel struct {
			Assets []releaseAsset `json:"assets"`
		}
		if json.Unmarshal(body, &rel) == nil {
			if url := pickAsset(rel.Assets); url != "" {
				if data, err := getBytes(client, url, false); err == nil {
					return data, nil
				}
			}
		}
	}
	var lastErr error
	for _, branch := range []string{"main", "master"} {
		url := fmt.Sprintf("https://codeload.github.com/%s/%s/zip/refs/heads/%s", owner, repo, branch)
		data, err := getBytes(client, url, false)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("下载失败")
	}
	return nil, lastErr
}

func pickAsset(assets []releaseAsset) string {
	candidates := []string{runtime.GOOS + "-" + runtime.GOARCH, runtime.GOOS, "plugin", ""}
	for _, want := range candidates {
		for _, a := range assets {
			name := strings.ToLower(a.Name)
			if !strings.HasSuffix(name, ".zip") {
				continue
			}
			if want == "" || strings.Contains(name, want) {
				return a.URL
			}
		}
	}
	return ""
}

func getBytes(client *http.Client, url string, api bool) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mygo-video")
	if api {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载失败：%s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 100<<20))
}

// installDialogView asks for a GitHub address, and says how installing goes.
func (a *app) installDialogView(c *ui.Context) {
	if !a.installDialog {
		return
	}
	p := colors()
	ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).
		Background(ui.RGBA(0, 0, 0, 0.5)).Center().Children(func() {
		ui.Column(c).Width(520).Padding(22).Gap(12).Radius(12).
			Background(p.panel).Border(1, p.hairline).
			Shadow(0, 20, 48, 4, p.cardShade).Children(func() {
			ui.Text(c, "从 GitHub 安装插件").FontSize(15).FontWeight(600)
			ui.Text(c, "填仓库地址，如 owner/repo 或 https://github.com/owner/repo。取最新 release 的 zip；没有 release 就用默认分支。").
				FontSize(11.5).TextColor(p.muted)
			input := ui.TextInput(c, &a.installURL).Placeholder("owner/repo").Height(34)
			input.Disabled(a.installBusy)
			if a.installFocus {
				a.installFocus = false
				input.Focus()
			}
			if a.installBusy {
				ui.Text(c, "正在下载并安装…").FontSize(11).TextColor(p.faint)
			}
			if a.installErr != "" {
				ui.Text(c, a.installErr).FontSize(11).TextColor(ui.Hex("#ef8f7a"))
			}
			ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
				ghostButton(c, "取消", nil, func() {
					a.installDialog = false
					a.installErr = ""
				})
				primaryButton(c, "安装", iconArchive, a.installGitHub)
			})
		})
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

// pluginEntryByID is the plugin with that id, on or off.
func (a *app) pluginEntryByID(id string) *pluginEntry {
	for _, pe := range a.plugins {
		if pe.man.ID == id {
			return pe
		}
	}
	return nil
}
