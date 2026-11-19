package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

// dataDirPointer is the file that says where the data lives. It stays in the
// default place, so the app finds it before it has a data directory.
func dataDirPointer() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, mygo.App.Name(), "data-dir.txt")
}

// readDataDirPointer is the directory the listener chose, or "".
func readDataDirPointer() string {
	path := dataDirPointer()
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(b))
	if dir == "" {
		return ""
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return ""
	}
	return dir
}

func writeDataDirPointer(dir string) error {
	path := dataDirPointer()
	if path == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(dir+"\n"), 0o600)
}

func clearDataDirPointer() error {
	path := dataDirPointer()
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
	if err := clearDataDirPointer(); err != nil {
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
