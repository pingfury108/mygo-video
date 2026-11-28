package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const dataDirCleanupName = "move-cleanup.json"

// dataDirCopyNames is what travels to the new directory.
var dataDirCopyNames = []string{
	"state.json",
	"webdav-cache.json",
	"plugins",
	"plugin-data",
}

// dataDirOldFiles is app-owned data left behind after a move. Videos and
// unknown files stay where they are.
var dataDirOldFiles = []string{
	"state.json.part",
	"webdav-cache.json.part",
	pointerName + ".part",
	"diag.log",
	"startup.log",
	"startup-error.log",
}

// dataDirOwnedFiles are app files at the root of a directory the app owns.
// They are kept in a directory the listener may share with something else.
var dataDirOwnedFiles = []string{
	"window-state.json",
	"window-state.json.part",
	"updater.json",
	"updater.json.part",
}

// dataDirCacheNames are directories the app uses, but a listener may have
// chosen a shared folder where these names already belonged to something
// else. They are removed only from a directory the app clearly owns.
var dataDirCacheNames = []string{
	"thumbs",
	"cache",
	"logs",
	"tmp",
}

type dataDirCleanup struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Digest string `json:"digest"`
}

// clearDataDirCleanup drops a marker that does not belong to the switch now
// being made, so an older move cannot delete a directory this one means to
// keep.
func clearDataDirCleanup(dir string) error {
	err := os.Remove(filepath.Join(dir, dataDirCleanupName))
	if os.IsNotExist(err) {
		return nil
	}
	return err
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

// verifyDataDirContents compares the bytes of every entry the source holds,
// so a source that changed during the copy is not taken for a complete move.
// The digest covers the whole target, including anything already there, for
// the restarted app to check before it deletes the old directory.
func verifyDataDirContents(from, to string) (string, error) {
	for _, name := range dataDirCopyNames {
		src := filepath.Join(from, name)
		info, err := os.Stat(src)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := compareCopiedPath(from, to, src, info); err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
	}
	return dataDirDigest(to)
}

func compareCopiedPath(fromBase, toBase, srcPath string, srcInfo os.FileInfo) error {
	rel, err := filepath.Rel(fromBase, srcPath)
	if err != nil {
		return err
	}
	dstPath := filepath.Join(toBase, rel)
	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		return err
	}
	if srcInfo.IsDir() != dstInfo.IsDir() {
		return fmt.Errorf("%s 类型不一致", filepath.ToSlash(rel))
	}
	if srcInfo.IsDir() {
		entries, err := os.ReadDir(srcPath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			child := filepath.Join(srcPath, entry.Name())
			childInfo, err := os.Stat(child)
			if err != nil {
				return err
			}
			if err := compareCopiedPath(fromBase, toBase, child, childInfo); err != nil {
				return err
			}
		}
		return nil
	}
	if !srcInfo.Mode().IsRegular() {
		return fmt.Errorf("%s 不是普通文件", filepath.ToSlash(rel))
	}
	if !dstInfo.Mode().IsRegular() {
		return fmt.Errorf("%s 不是普通文件", filepath.ToSlash(rel))
	}
	if srcInfo.Size() != dstInfo.Size() {
		return fmt.Errorf("%s 大小不一致：%d != %d", filepath.ToSlash(rel), srcInfo.Size(), dstInfo.Size())
	}
	srcSum, srcSize, err := fileDigest(srcPath)
	if err != nil {
		return err
	}
	if srcSize != srcInfo.Size() {
		return fmt.Errorf("%s 在读取时发生了变化", filepath.ToSlash(rel))
	}
	dstSum, dstSize, err := fileDigest(dstPath)
	if err != nil {
		return err
	}
	if dstSize != dstInfo.Size() {
		return fmt.Errorf("%s 在读取时发生了变化", filepath.ToSlash(rel))
	}
	if string(srcSum) != string(dstSum) {
		return fmt.Errorf("%s 内容不一致", filepath.ToSlash(rel))
	}
	return nil
}

func fileDigest(path string) ([]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return nil, 0, err
	}
	return h.Sum(nil), n, nil
}

// dataDirDigest fingerprints what a data directory holds, with paths, sizes
// and bytes, so the restarted app can tell whether the target is still whole
// before it deletes the old directory.
func dataDirDigest(dir string) (string, error) {
	h := sha256.New()
	for _, name := range dataDirCopyNames {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := digestDataDirPath(h, dir, path, info); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func digestDataDirPath(h io.Writer, base, path string, info os.FileInfo) error {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if info.IsDir() {
		fmt.Fprintf(h, "dir %q\n", rel)
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			child := filepath.Join(path, entry.Name())
			childInfo, err := os.Lstat(child)
			if err != nil {
				return err
			}
			if err := digestDataDirPath(h, base, child, childInfo); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s 不是普通文件", rel)
	}
	fmt.Fprintf(h, "file %q %d\n", rel, info.Size())
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if n != info.Size() {
		return fmt.Errorf("%s 在读取时发生了变化", rel)
	}
	fmt.Fprintln(h)
	return nil
}

// writeDataDirCleanup leaves the cleanup for the restarted process, after the
// old instance has released its files.
func writeDataDirCleanup(to, from, digest string) error {
	b, err := json.Marshal(dataDirCleanup{From: from, To: to, Digest: digest})
	if err != nil {
		return err
	}
	path := filepath.Join(to, dataDirCleanupName)
	part := path + ".part"
	if err := os.WriteFile(part, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(part, path); err != nil {
		_ = os.Remove(part)
		return err
	}
	return nil
}

// finishDataDirMove cleans the old directory once the restarted app is running
// from the new one.
func (a *app) finishDataDirMove() {
	path := filepath.Join(a.dataDir, dataDirCleanupName)
	_ = os.Remove(path + ".part")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var c dataDirCleanup
	if json.Unmarshal(b, &c) != nil ||
		!filepath.IsAbs(c.From) ||
		!filepath.IsAbs(c.To) ||
		c.Digest == "" ||
		filepath.Dir(filepath.Clean(c.From)) == filepath.Clean(c.From) ||
		!sameDir(c.To, a.dataDir) ||
		!looksLikeAppDataDir(c.From) ||
		sameDir(c.From, c.To) ||
		sameExistingDir(c.From, c.To) ||
		withinDir(c.From, c.To) ||
		withinDir(c.To, c.From) {
		_ = os.Remove(path)
		return
	}
	digest, err := dataDirDigest(c.To)
	if err != nil || digest != c.Digest {
		_ = os.Remove(path)
		a.diag("move cleanup: target changed: %v", err)
		a.say("新数据目录校验失败，旧数据已保留")
		return
	}
	keptCaches, err := removeOldDataDir(c.From)
	if err != nil {
		a.diag("move cleanup: %v", err)
		a.say("移动已完成，但旧数据没有清理干净：%v", err)
		return
	}
	_ = os.Remove(path)
	if keptCaches {
		a.say("数据已移动，旧目录里的缓存和日志已保留，可手动清理")
		return
	}
	a.say("数据已移动，旧应用数据已清理")
}

// looksLikeAppDataDir keeps a stray or damaged marker from deleting a
// directory this app never owned.
func looksLikeAppDataDir(dir string) bool {
	for _, name := range dataDirCopyNames {
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// removeOldDataDir removes only data this app owns. A videos directory or any
// file it does not know stays in place. It reports whether cache and log
// folders were kept because the chosen directory may be shared.
func removeOldDataDir(dir string) (bool, error) {
	for _, name := range dataDirCopyNames {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return false, fmt.Errorf("%s: %w", name, err)
		}
	}
	for _, name := range dataDirOldFiles {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return false, fmt.Errorf("%s: %w", name, err)
		}
	}
	kept := false
	if ownsWholeDataDir(dir) {
		for _, name := range dataDirOwnedFiles {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
				return false, fmt.Errorf("%s: %w", name, err)
			}
		}
		for _, name := range dataDirCacheNames {
			if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
				return false, fmt.Errorf("%s: %w", name, err)
			}
		}
	} else {
		for _, name := range dataDirCacheNames {
			if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
				kept = true
				break
			}
		}
	}
	if !sameDir(dir, defaultBaseDirPath()) {
		_ = os.Remove(filepath.Join(dir, pointerName))
	}
	removeEmptyDir(filepath.Join(dir, "videos"))
	removeEmptyDir(dir)
	return kept, nil
}

// removeEmptyDir removes a real empty directory, never a symlink: a videos
// link or a chosen directory that points elsewhere belongs to the listener.
func removeEmptyDir(path string) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	_ = os.Remove(path)
}

// ownsWholeDataDir says the app may clean cache, log and temporary folders in
// dir too: its own default location, or a folder named after the app. A
// listener can choose any folder, where those names may be someone else's.
func ownsWholeDataDir(dir string) bool {
	if sameDir(dir, defaultBaseDirPath()) {
		return true
	}
	return strings.EqualFold(filepath.Base(filepath.Clean(dir)), baseName())
}
