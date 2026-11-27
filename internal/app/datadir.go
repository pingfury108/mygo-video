package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
)

// pointerName is the small bootstrap file in the default directory. It has to
// live where startup can find it before it knows the chosen data directory.
const pointerName = "data-dir.txt"

func writeDataDirPointer(dir string) error {
	return writeDataDirPointerAt(defaultBaseDirPath(), dir)
}

func writeDataDirPointerAt(baseDir, dir string) error {
	if dir == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(baseDir, pointerName)
	if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) == dir {
		return nil
	}
	part := path + ".part"
	if err := os.WriteFile(part, []byte(dir+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(part, path); err != nil {
		_ = os.Remove(part)
		return err
	}
	return nil
}

func (a *app) clearDataDirPointer() error {
	paths := []string{filepath.Join(defaultBaseDirPath(), pointerName)}
	if a.dataDir != "" && !sameDir(a.dataDir, defaultBaseDirPath()) {
		paths = append(paths, filepath.Join(a.dataDir, pointerName))
	}
	for _, path := range paths {
		_ = os.Remove(path + ".part")
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// tempDir keeps files that only matter while the app runs beside its data,
// so playback and plugin installs use the drive the listener chose.
func (a *app) tempDir() string {
	dir := a.dataDir
	if dir == "" {
		dir = baseDir()
	}
	return filepath.Join(dir, "tmp")
}

// changeDataDir asks where to keep the data, copies what the app has there,
// and restarts into it.
func (a *app) changeDataDir() {
	if a.dataDirBusy {
		return
	}
	go func() {
		dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      a.win,
			Title:       "选择数据目录",
			ButtonLabel: "使用",
			Message:     "观看记录、插件和设置会放在这里",
			Directory:   true,
		})
		if err != nil || len(dirs) == 0 {
			return
		}
		dir := dirs[0]
		if sameDir(dir, a.dataDir) {
			return
		}
		mygo.RunOnMain(func() {
			if a.dataDirBusy {
				return
			}
			res, _ := mygo.Dialog.Message(mygo.MessageOptions{
				Parent:  a.win,
				Message: "要把现有的数据一起带过去吗？",
				Detail:  "复制会带过去观看记录、插件、插件数据和 WebDAV 缓存，源目录保留。移动会先复制并逐字节校验，重启后再清理旧目录里的应用数据；缓存、日志和临时文件只在应用专属目录里删除，选择共享目录时会保留。视频文件、未知文件和非本应用的数据不会删除。两种操作都会停止播放，目标里的同名文件会被覆盖。",
				Buttons: []string{"复制", "移动并删除旧数据", "只切换", "取消"},
			})
			switch res.Button {
			case 0:
				a.copyDataDirAndRestart(dir, false)
			case 1:
				confirm, _ := mygo.Dialog.Message(mygo.MessageOptions{
					Parent:             a.win,
					Message:            "移动并删除旧数据？",
					Detail:             "会先复制并校验应用数据，切换成功后自动重启，再清理旧目录。视频文件、未知文件和非本应用的数据不会删除。",
					Buttons:            []string{"开始移动", "取消"},
					DestructiveButtons: []int{0},
				})
				if confirm.Button == 0 {
					a.copyDataDirAndRestart(dir, true)
				}
			case 2:
				if err := clearDataDirCleanup(dir); err != nil {
					a.say("切换失败：%v", err)
					return
				}
				a.useDataDir(dir)
			}
		})
	}()
}

// copyDataDirAndRestart copies the app's data, then points the app at the new
// directory. Playback stays stopped until the app restarts.
func (a *app) copyDataDirAndRestart(dir string, removeOld bool) {
	if a.dataDirBusy {
		return
	}
	from := a.dataDir
	if err := validateDataDirCopy(from, dir); err != nil {
		a.say("%v", err)
		return
	}
	action := "复制"
	if removeOld {
		action = "移动"
	}
	// A marker left by an earlier move belongs to that move. Drop it before
	// copying so a later start never deletes a directory this copy means to
	// keep.
	if err := clearDataDirCleanup(dir); err != nil {
		a.say("%s失败：%v", action, err)
		return
	}

	a.dataDirBusy = true
	a.dataDirStatus = "正在停止播放…"
	a.playSeq++
	a.stopPlaybackForMove()
	a.closePluginsForMove()
	if a.win != nil && a.win.IsFullScreen() {
		a.win.SetFullScreen(false)
	}
	a.route = pageSettings
	a.flushDataForMove()
	a.dataDirStatus = "正在" + action + "应用数据…"
	a.invalidate()

	go func() {
		err := copyDataDir(from, dir)
		if err == nil {
			err = verifyDataDirCopy(from, dir)
		}
		var digest string
		if err == nil {
			digest, err = verifyDataDirContents(from, dir)
		}
		if err == nil && removeOld {
			err = writeDataDirCleanup(dir, from, digest)
		}
		mygo.RunOnMain(func() {
			if err != nil {
				a.dataDirBusy = false
				a.dataDirStatus = ""
				a.loadPlugins()
				a.loadPluginCookies()
				a.say("%s失败：%v", action, err)
				a.invalidate()
				return
			}
			// Keep playback locked until the process leaves this directory:
			// the relaunch is queued, and a click in between must not start
			// a video whose progress would be written to the old place.
			a.dataDirStatus = "正在重启…"
			a.dataDirQuitAllowed = true
			a.invalidate()
			if !a.useDataDir(dir) {
				a.dataDirQuitAllowed = false
				a.dataDirBusy = false
				a.dataDirStatus = ""
				a.loadPlugins()
				a.loadPluginCookies()
				a.invalidate()
			}
		})
	}()
}

// stopPlaybackForMove stops every way a video can be playing before its data
// directory changes.
func (a *app) stopPlaybackForMove() {
	a.cancelWebDAVScan()
	a.webdavPath = ""
	if a.mpvProcess != nil {
		_ = a.mpvProcess.Kill()
		a.mpvProcess = nil
	}
	a.mpvPlaying = false
	a.closePlayer()
	a.stopLiveDanmaku()
	a.releaseDisplayLock()
}

// closePluginsForMove stops plugin code that may still write plugin data while
// it is copied.
func (a *app) closePluginsForMove() {
	for _, pe := range a.plugins {
		if pe.p != nil {
			pe.p.Close()
			pe.p = nil
		}
	}
}

// flushDataForMove makes the copy include what the current run has in memory.
func (a *app) flushDataForMove() {
	a.saveProgress()
	if a.store != nil {
		a.store.SetVolume(a.volume)
		a.store.Save()
		a.saveWebDAVCache()
	}
}

// useDataDir points the app at dir, and restarts into it.
func (a *app) useDataDir(dir string) bool {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		a.say("建不了这个目录：%v", err)
		return false
	}
	probe, err := os.CreateTemp(dir, ".mygo-video-write-")
	if err != nil {
		a.say("这个目录不能写入：%v", err)
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	if err := writeDataDirPointer(dir); err != nil {
		a.say("记不住这个目录：%v", err)
		return false
	}
	mygo.App.Relaunch()
	return true
}

// resetDataDir goes back to the default directory.
func (a *app) resetDataDir() {
	if a.dataDirBusy {
		return
	}
	if err := clearDataDirCleanup(defaultBaseDirPath()); err != nil {
		a.say("改不回去：%v", err)
		return
	}
	if err := a.clearDataDirPointer(); err != nil {
		a.say("改不回去：%v", err)
		return
	}
	mygo.App.Relaunch()
}

// copyDataDir copies what a data directory holds besides caches.
func copyDataDir(from, to string) error {
	if err := validateDataDirCopy(from, to); err != nil {
		return err
	}
	if err := os.MkdirAll(to, 0o700); err != nil {
		return err
	}
	for _, name := range dataDirCopyNames {
		src := filepath.Join(from, name)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		if err := copyPath(src, filepath.Join(to, name)); err != nil {
			return err
		}
	}
	return nil
}

func validateDataDirCopy(from, to string) error {
	if from == "" || to == "" {
		return errors.New("数据目录为空")
	}
	if sameDir(from, to) || sameExistingDir(from, to) || withinDir(from, to) || withinDir(to, from) {
		return errors.New("目标目录不能与当前数据目录相同或互相包含")
	}
	return nil
}

func sameExistingDir(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// withinDir says whether child is inside parent, not parent itself.
func withinDir(parent, child string) bool {
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		parent = resolved
	}
	if resolved, err := filepath.EvalSymlinks(child); err == nil {
		child = resolved
	}
	p, c := filepath.Clean(parent), filepath.Clean(child)
	if runtime.GOOS == "windows" {
		p, c = strings.ToLower(p), strings.ToLower(c)
	}
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func copyPath(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if st.IsDir() {
		if err := os.MkdirAll(dst, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	part := dst + ".part"
	out, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(part)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(part)
		return err
	}
	if err := os.Rename(part, dst); err != nil {
		_ = os.Remove(part)
		return err
	}
	return nil
}
