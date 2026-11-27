package app

import (
	"io"
	"os"
	"path/filepath"

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
	return os.WriteFile(filepath.Join(baseDir, pointerName), []byte(dir+"\n"), 0o600)
}

func (a *app) clearDataDirPointer() error {
	paths := []string{filepath.Join(defaultBaseDirPath(), pointerName)}
	if a.dataDir != "" && !sameDir(a.dataDir, defaultBaseDirPath()) {
		paths = append(paths, filepath.Join(a.dataDir, pointerName))
	}
	for _, path := range paths {
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
		if dir == a.dataDir {
			return
		}
		mygo.RunOnMain(func() {
			res, _ := mygo.Dialog.Message(mygo.MessageOptions{
				Parent:  a.win,
				Message: "把现有的数据也移过去？",
				Detail:  "「移动」复制观看记录、插件和设置；缩略图缓存会重新生成。",
				Buttons: []string{"移动", "只切换", "取消"},
			})
			switch res.Button {
			case 0:
				if err := copyDataDir(a.dataDir, dir); err != nil {
					a.say("复制失败：%v", err)
					return
				}
				a.useDataDir(dir)
			case 1:
				a.useDataDir(dir)
			}
		})
	}()
}

// useDataDir points the app at dir, and restarts into it.
func (a *app) useDataDir(dir string) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		a.say("建不了这个目录：%v", err)
		return
	}
	if err := writeDataDirPointer(dir); err != nil {
		a.say("记不住这个目录：%v", err)
		return
	}
	mygo.App.Relaunch()
}

// resetDataDir goes back to the default directory.
func (a *app) resetDataDir() {
	if err := a.clearDataDirPointer(); err != nil {
		a.say("改不回去：%v", err)
		return
	}
	mygo.App.Relaunch()
}

// copyDataDir copies what a data directory holds besides caches.
func copyDataDir(from, to string) error {
	if err := os.MkdirAll(to, 0o700); err != nil {
		return err
	}
	for _, name := range []string{"state.json", "webdav-cache.json", "plugins", "plugin-data"} {
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
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
