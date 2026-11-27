package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
)

// The system locations, taken before Windows points the process at the data
// directory. The HEVC extension is installed under the real LOCALAPPDATA,
// even when the app writes its own files elsewhere.
var (
	windowsSystemAPPDATA      string
	windowsSystemLocalAppData string
	defaultBaseDir            string
	currentBaseDir            string
	baseDirCustom             bool
)

// prepareBaseDirs chooses the root every kind of Windows app file uses. A
// listener who picked a directory gets its data, caches, logs and temporary
// files there; one who did not gets the default data directory. It must run
// before the single-instance lock, WebView2, the GPU and the updater. Other
// systems keep MyGo's standard paths.
func prepareBaseDirs() (string, bool, error) {
	windowsSystemAPPDATA, _ = os.UserConfigDir()
	if runtime.GOOS == "windows" {
		windowsSystemLocalAppData = os.Getenv("LOCALAPPDATA")
	}
	defaultBaseDir = filepath.Join(windowsSystemAPPDATA, baseName())

	dir := resolveBaseDir(defaultBaseDir)
	custom := !sameDir(dir, defaultBaseDir)
	if runtime.GOOS == "windows" {
		cache, logs, tmp := filepath.Join(dir, "cache"), filepath.Join(dir, "logs"), filepath.Join(dir, "tmp")
		for _, path := range []string{cache, logs, tmp} {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return dir, custom, err
			}
		}
		if custom {
			if err := os.WriteFile(filepath.Join(dir, pointerName), []byte(dir+"\n"), 0o600); err != nil {
				dir, custom = defaultBaseDir, false
			}
		}
		os.Setenv("APPDATA", dir)
		os.Setenv("LOCALAPPDATA", filepath.Join(dir, "cache"))
		os.Setenv("TMP", filepath.Join(dir, "tmp"))
		os.Setenv("TEMP", filepath.Join(dir, "tmp"))
		mygo.App.SetPath(mygo.PathCache, filepath.Join(dir, "cache"))
		mygo.App.SetPath(mygo.PathLogs, filepath.Join(dir, "logs"))
		mygo.App.SetPath(mygo.PathTemp, filepath.Join(dir, "tmp"))
	} else if custom {
		if err := os.WriteFile(filepath.Join(dir, pointerName), []byte(dir+"\n"), 0o600); err != nil {
			dir, custom = defaultBaseDir, false
		}
	}
	mygo.App.SetPath(mygo.PathUserData, dir)
	currentBaseDir = dir
	return dir, custom, nil
}

// resolveBaseDir follows the pointer kept inside the data directory, after
// falling back to the pointer older versions left in the default one. A
// directory that has gone away is ignored, and a cycle cannot run forever.
func resolveBaseDir(defaultDir string) string {
	seen := map[string]bool{}
	dir := defaultDir
	for range 8 {
		if seen[sameKey(dir)] {
			break
		}
		seen[sameKey(dir)] = true
		next, ok := pointerTarget(dir, pointerName)
		if !ok {
			next, ok = pointerTarget(defaultDir, legacyPointerName)
		}
		if !ok || sameDir(next, dir) {
			break
		}
		dir = next
	}
	return dir
}

// pointerTarget reads one pointer file and accepts only a real directory.
func pointerTarget(dir, name string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", false
	}
	next := strings.TrimSpace(string(b))
	if next == "" {
		return "", false
	}
	st, err := os.Stat(next)
	if err != nil || !st.IsDir() {
		return "", false
	}
	return next, true
}

// baseName is the directory every app kind uses. The development build
// stays apart from the installed one, as MyGo does elsewhere.
func baseName() string {
	if mygo.IsDev() {
		return appName + " Dev"
	}
	return appName
}

// systemLocalAppData is the machine's own cache location, not the one the
// app pointed at its data directory.
func systemLocalAppData() string {
	if windowsSystemLocalAppData != "" {
		return windowsSystemLocalAppData
	}
	return os.Getenv("LOCALAPPDATA")
}

// defaultBaseDirPath is where an app without a chosen directory belongs.
func defaultBaseDirPath() string {
	if defaultBaseDir != "" {
		return defaultBaseDir
	}
	base := windowsSystemAPPDATA
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, baseName())
}

// baseDir is the root this run redirects to.
func baseDir() string {
	if currentBaseDir != "" {
		return currentBaseDir
	}
	return defaultBaseDirPath()
}

// sameDir compares two paths the way Windows names drives and folders.
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func sameKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

// legacyPointerName keeps a first upgrade working before the pointer moved.
const legacyPointerName = "data-dir.txt"
