package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDataDirPointerIsFoundAfterRestart(t *testing.T) {
	defaultDir := t.TempDir()
	target := t.TempDir()

	if err := writeDataDirPointerAt(defaultDir, target); err != nil {
		t.Fatal(err)
	}
	if got, _ := resolveBaseDir(defaultDir); !sameDir(got, target) {
		t.Fatalf("resolveBaseDir() = %q, want %q", got, target)
	}
}

func TestDataDirPointerFollowsExistingChain(t *testing.T) {
	defaultDir := t.TempDir()
	older := t.TempDir()
	target := t.TempDir()

	if err := writeDataDirPointerAt(defaultDir, older); err != nil {
		t.Fatal(err)
	}
	if err := writeDataDirPointerAt(older, target); err != nil {
		t.Fatal(err)
	}
	if got, _ := resolveBaseDir(defaultDir); !sameDir(got, target) {
		t.Fatalf("resolveBaseDir() = %q, want %q", got, target)
	}
}

func TestDataDirPointerKeepsBootstrapFileSmall(t *testing.T) {
	defaultDir := t.TempDir()
	target := t.TempDir()

	if err := writeDataDirPointerAt(defaultDir, target); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(defaultDir, pointerName))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != target+"\n" {
		t.Fatalf("pointer = %q, want %q", b, target+"\n")
	}
}

func TestResolveBaseDirRecoversNestedPointer(t *testing.T) {
	defaultDir := t.TempDir()
	nested := filepath.Join(defaultDir, baseName())
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := writeDataDirPointerAt(nested, target); err != nil {
		t.Fatal(err)
	}

	if got, recovered := resolveBaseDir(defaultDir); !sameDir(got, target) || !recovered {
		t.Fatalf("resolveBaseDir() = %q, want the nested pointer target %q", got, target)
	}
}

func TestResolveBaseDirPrefersNewestPointer(t *testing.T) {
	defaultDir := t.TempDir()
	nested := filepath.Join(defaultDir, baseName())
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	older := t.TempDir()
	newer := t.TempDir()
	if err := writeDataDirPointerAt(defaultDir, older); err != nil {
		t.Fatal(err)
	}
	if err := writeDataDirPointerAt(nested, newer); err != nil {
		t.Fatal(err)
	}
	oldAt := time.Now().Add(-time.Hour)
	newAt := time.Now()
	if err := os.Chtimes(filepath.Join(defaultDir, pointerName), oldAt, oldAt); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(nested, pointerName), newAt, newAt); err != nil {
		t.Fatal(err)
	}

	if got, recovered := resolveBaseDir(defaultDir); !sameDir(got, newer) || !recovered {
		t.Fatalf("resolveBaseDir() = %q, want the newest pointer target %q", got, newer)
	}
}

func TestResolveBaseDirIgnoresNestedDefaultTarget(t *testing.T) {
	defaultDir := t.TempDir()
	nested := filepath.Join(defaultDir, baseName())
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeDataDirPointerAt(defaultDir, nested); err != nil {
		t.Fatal(err)
	}

	if got, _ := resolveBaseDir(defaultDir); !sameDir(got, defaultDir) {
		t.Fatalf("resolveBaseDir() = %q, want the default directory %q", got, defaultDir)
	}
}

func TestCopyDataDirCopiesAppDataAndKeepsSource(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	writeTestFile(t, filepath.Join(from, "state.json"), "state")
	writeTestFile(t, filepath.Join(from, "webdav-cache.json"), "cache")
	writeTestFile(t, filepath.Join(from, "plugins", "demo", "plugin.json"), "plugin")
	writeTestFile(t, filepath.Join(from, "plugin-data", "demo", "cookies.json"), "cookies")
	writeTestFile(t, filepath.Join(from, "videos", "movie.mp4"), "video")
	writeTestFile(t, filepath.Join(from, "thumbs", "frame.bin"), "thumb")
	writeTestFile(t, filepath.Join(from, "logs", "diag.log"), "log")
	writeTestFile(t, filepath.Join(from, "tmp", "part"), "tmp")

	if err := copyDataDir(from, to); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"state.json",
		filepath.Join("webdav-cache.json"),
		filepath.Join("plugins", "demo", "plugin.json"),
		filepath.Join("plugin-data", "demo", "cookies.json"),
	} {
		if _, err := os.Stat(filepath.Join(to, name)); err != nil {
			t.Errorf("%s was not copied: %v", name, err)
		}
	}
	for _, name := range []string{
		filepath.Join("videos", "movie.mp4"),
		filepath.Join("thumbs", "frame.bin"),
		filepath.Join("logs", "diag.log"),
		filepath.Join("tmp", "part"),
	} {
		if _, err := os.Stat(filepath.Join(to, name)); !os.IsNotExist(err) {
			t.Errorf("%s should not be copied", name)
		}
	}
	if _, err := os.Stat(filepath.Join(from, "state.json")); err != nil {
		t.Fatalf("source was changed: %v", err)
	}
}

func TestCopyDataDirRejectsOverlappingDirectories(t *testing.T) {
	from := t.TempDir()
	inside := filepath.Join(from, "inside")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{from, inside} {
		if err := copyDataDir(from, to); err == nil {
			t.Errorf("copyDataDir(%q, %q) should fail", from, to)
		}
	}
	if err := copyDataDir(inside, from); err == nil {
		t.Errorf("copyDataDir(%q, %q) should fail", inside, from)
	}
}

func TestVerifyDataDirCopyChecksEveryFile(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	writeTestFile(t, filepath.Join(from, "state.json"), "state")
	writeTestFile(t, filepath.Join(from, "plugins", "demo", "plugin.json"), "plugin")
	if err := copyDataDir(from, to); err != nil {
		t.Fatal(err)
	}
	if err := verifyDataDirCopy(from, to); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(to, "plugins", "demo", "plugin.json"), "changed")
	if err := verifyDataDirCopy(from, to); err == nil {
		t.Fatal("verifyDataDirCopy accepted a file of a different size")
	}
}

func TestRemoveOldDataDirKeepsVideosAndUnknownFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"state.json",
		"webdav-cache.json",
		filepath.Join("plugins", "demo", "plugin.json"),
		filepath.Join("plugin-data", "demo", "cookies.json"),
		"thumbs/frame.bin",
		"cache/data.bin",
		"logs/diag.log",
		"tmp/part",
		"diag.log",
		"startup.log",
		"startup-error.log",
	} {
		writeTestFile(t, filepath.Join(dir, name), name)
	}
	writeTestFile(t, filepath.Join(dir, "videos", "movie.mp4"), "video")
	writeTestFile(t, filepath.Join(dir, "notes.txt"), "keep")

	kept, err := removeOldDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !kept {
		t.Error("cache folders in a shared directory should have been kept")
	}
	for _, name := range []string{
		"state.json",
		"webdav-cache.json",
		"plugins",
		"plugin-data",
		"diag.log",
		"startup.log",
		"startup-error.log",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", name)
		}
	}
	for _, name := range []string{
		"videos/movie.mp4",
		"notes.txt",
		"thumbs/frame.bin",
		"cache/data.bin",
		"logs/diag.log",
		"tmp/part",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s should have been kept: %v", name, err)
		}
	}
}

func TestRemoveOldDataDirCleansCachesInOwnedDir(t *testing.T) {
	dir := t.TempDir()
	oldDefault := defaultBaseDir
	defaultBaseDir = dir
	t.Cleanup(func() { defaultBaseDir = oldDefault })

	for _, name := range []string{
		"state.json",
		"thumbs/frame.bin",
		"cache/data.bin",
		"logs/diag.log",
		"tmp/part",
	} {
		writeTestFile(t, filepath.Join(dir, name), name)
	}

	kept, err := removeOldDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if kept {
		t.Error("cache folders in the app's own directory should have been removed")
	}
	for _, name := range []string{"thumbs", "cache", "logs", "tmp"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", name)
		}
	}
}

func TestFinishDataDirMoveRunsCleanupAfterRestart(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	writeTestFile(t, filepath.Join(from, "state.json"), "old state")
	writeTestFile(t, filepath.Join(from, "videos", "movie.mp4"), "video")
	if err := copyDataDir(from, to); err != nil {
		t.Fatal(err)
	}
	digest, err := verifyDataDirContents(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDataDirCleanup(to, from, digest); err != nil {
		t.Fatal(err)
	}

	a := &app{dataDir: to}
	a.finishDataDirMove()

	if _, err := os.Stat(filepath.Join(from, "state.json")); !os.IsNotExist(err) {
		t.Error("old state.json was not removed")
	}
	if _, err := os.Stat(filepath.Join(from, "videos", "movie.mp4")); err != nil {
		t.Errorf("old video was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(to, dataDirCleanupName)); !os.IsNotExist(err) {
		t.Error("cleanup marker was not removed")
	}
}

func TestFinishDataDirMoveKeepsOldDataWhenTargetChanged(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	writeTestFile(t, filepath.Join(from, "state.json"), "state")
	if err := copyDataDir(from, to); err != nil {
		t.Fatal(err)
	}
	digest, err := dataDirDigest(to)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDataDirCleanup(to, from, digest); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(to, "state.json"), "changed after the copy")

	a := &app{dataDir: to}
	a.finishDataDirMove()

	if _, err := os.Stat(filepath.Join(from, "state.json")); err != nil {
		t.Errorf("old data was removed after the target changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(to, dataDirCleanupName)); !os.IsNotExist(err) {
		t.Error("cleanup marker was not removed")
	}
}

func TestFinishDataDirMoveIgnoresUnknownSource(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	writeTestFile(t, filepath.Join(from, "notes.txt"), "keep")
	writeTestFile(t, filepath.Join(to, "state.json"), "state")
	digest, err := dataDirDigest(to)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDataDirCleanup(to, from, digest); err != nil {
		t.Fatal(err)
	}

	a := &app{dataDir: to}
	a.finishDataDirMove()

	if _, err := os.Stat(filepath.Join(from, "notes.txt")); err != nil {
		t.Errorf("a directory this app did not own was changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(to, dataDirCleanupName)); !os.IsNotExist(err) {
		t.Error("cleanup marker was not removed")
	}
}

func TestVerifyDataDirContentsCatchesSameSizeChange(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	writeTestFile(t, filepath.Join(from, "state.json"), "aaaa")
	if err := copyDataDir(from, to); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(to, "state.json"), "bbbb")
	if _, err := verifyDataDirContents(from, to); err == nil {
		t.Fatal("verifyDataDirContents accepted changed bytes of the same size")
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
