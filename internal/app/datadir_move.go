package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const dataDirCleanupName = "move-cleanup.json"

// dataDirCopyNames is what travels to the new directory.
var dataDirCopyNames = []string{
	"state.json",
	"webdav-cache.json",
	"plugins",
	"plugin-data",
}

// dataDirOldOnlyNames is app-owned data left behind after a move. Videos and
// unknown files stay where they are.
var dataDirOldOnlyNames = []string{
	"state.json.part",
	"webdav-cache.json.part",
	"thumbs",
	"cache",
	"logs",
	"tmp",
	"diag.log",
	"startup.log",
	"startup-error.log",
}

type dataDirCleanup struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// verifyDataDirCopy checks that every copied entry has the same structure and
// file sizes as its source before the old directory may be cleaned.
func verifyDataDirCopy(from, to string) error {
	for _, name := range dataDirCopyNames {
		src := filepath.Join(from, name)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		if err := verifyCopiedPath(src, filepath.Join(to, name)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func verifyCopiedPath(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return err
	}
	if srcInfo.IsDir() != dstInfo.IsDir() {
		return fmt.Errorf("类型不一致")
	}
	if !srcInfo.IsDir() {
		if srcInfo.Size() != dstInfo.Size() {
			return fmt.Errorf("大小不一致：%d != %d", srcInfo.Size(), dstInfo.Size())
		}
		return nil
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := verifyCopiedPath(
			filepath.Join(src, entry.Name()),
			filepath.Join(dst, entry.Name()),
		); err != nil {
			return err
		}
	}
	return nil
}

// writeDataDirCleanup leaves the cleanup for the restarted process, after the
// old instance has released its files.
func writeDataDirCleanup(to, from string) error {
	b, err := json.Marshal(dataDirCleanup{From: from, To: to})
	if err != nil {
		return err
	}
	path := filepath.Join(to, dataDirCleanupName)
	part := path + ".part"
	if err := os.WriteFile(part, b, 0o600); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(part, path)
}

// finishDataDirMove cleans the old directory once the restarted app is running
// from the new one.
func (a *app) finishDataDirMove() {
	path := filepath.Join(a.dataDir, dataDirCleanupName)
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var c dataDirCleanup
	if json.Unmarshal(b, &c) != nil ||
		!filepath.IsAbs(c.From) ||
		!filepath.IsAbs(c.To) ||
		filepath.Dir(filepath.Clean(c.From)) == filepath.Clean(c.From) ||
		!sameDir(c.To, a.dataDir) ||
		sameDir(c.From, c.To) ||
		sameExistingDir(c.From, c.To) ||
		withinDir(c.From, c.To) ||
		withinDir(c.To, c.From) {
		_ = os.Remove(path)
		return
	}
	if err := removeOldDataDir(c.From); err != nil {
		a.diag("move cleanup: %v", err)
		a.say("移动已完成，但旧数据没有清理干净：%v", err)
		return
	}
	_ = os.Remove(path)
	a.say("数据已移动，旧应用数据已清理")
}

// removeOldDataDir removes only data this app owns. A videos directory or any
// file it does not know stays in place.
func removeOldDataDir(dir string) error {
	for _, name := range dataDirCopyNames {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	for _, name := range dataDirOldOnlyNames {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if !sameDir(dir, defaultBaseDirPath()) {
		_ = os.Remove(filepath.Join(dir, pointerName))
	}
	_ = os.Remove(filepath.Join(dir, "videos"))
	_ = os.Remove(dir)
	return nil
}
