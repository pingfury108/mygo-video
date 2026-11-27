package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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
	dataDirRecovered          bool
)

// prepareBaseDirs chooses the root every kind of Windows app file uses. A
// listener who picked a directory gets its data, caches, logs and temporary
// files there; one who did not gets the default data directory. It must run
// before the single-instance lock, WebView2, the GPU and the updater. Other
// systems keep MyGo's standard paths.
func prepareBaseDirs() (string, bool, error) {
	windowsSystemAPPDATA, _ = systemConfigDir()
	if runtime.GOOS == "windows" {
		windowsSystemLocalAppData, _ = systemLocalAppDataDir()
	}
	defaultBaseDir = filepath.Join(windowsSystemAPPDATA, baseName())

	dir, recovered := resolveBaseDir(defaultBaseDir)
	dataDirRecovered = recovered
	custom := !sameDir(dir, defaultBaseDir)
	if custom {
		if err := writeDataDirPointerAt(defaultBaseDir, dir); err != nil {
			dir, custom = defaultBaseDir, false
		}
	}
	if runtime.GOOS == "windows" {
		cache, logs, tmp := filepath.Join(dir, "cache"), filepath.Join(dir, "logs"), filepath.Join(dir, "tmp")
		for _, path := range []string{cache, logs, tmp} {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return dir, custom, err
			}
		}
		// APPDATA is where the bootstrap pointer lives. Changing it here
		// made a relaunch look for the pointer one level deeper, so leave
		// it alone; everything the app writes uses the paths below.
		os.Setenv("LOCALAPPDATA", filepath.Join(dir, "cache"))
		os.Setenv("TMP", filepath.Join(dir, "tmp"))
		os.Setenv("TEMP", filepath.Join(dir, "tmp"))
		mygo.App.SetPath(mygo.PathCache, filepath.Join(dir, "cache"))
		mygo.App.SetPath(mygo.PathLogs, filepath.Join(dir, "logs"))
		mygo.App.SetPath(mygo.PathTemp, filepath.Join(dir, "tmp"))
	}
	mygo.App.SetPath(mygo.PathUserData, dir)
	currentBaseDir = dir
	return dir, custom, nil
}

// resolveBaseDir follows the bootstrap pointer and any chain earlier versions
// left behind. It also recovers pointers that Windows builds left one level
// deeper when a relaunch inherited the data directory as APPDATA. A directory
// that has gone away is ignored, and a cycle cannot run forever.
func resolveBaseDir(defaultDir string) (string, bool) {
	best := defaultDir
	var bestAt time.Time
	bestDepth := 0
	dir := defaultDir
	seen := map[string]bool{}
	for depth := range 16 {
		if seen[sameKey(dir)] {
			break
		}
		seen[sameKey(dir)] = true
		target, at, ok := readDataDirPointer(dir)
		if ok && dataDirTargetOK(defaultDir, target) &&
			(at.After(bestAt) || (at.Equal(bestAt) && depth < bestDepth)) {
			best, bestAt, bestDepth = target, at, depth
		}
		next := filepath.Join(dir, baseName())
		if st, err := os.Stat(next); err != nil || !st.IsDir() {
			break
		}
		dir = next
	}
	return followDataDirPointers(best, defaultDir), bestDepth > 0
}

// readDataDirPointer reads one bootstrap pointer and accepts only an existing
// absolute directory.
func readDataDirPointer(dir string) (string, time.Time, bool) {
	path := filepath.Join(dir, pointerName)
	st, err := os.Stat(path)
	if err != nil {
		return "", time.Time{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", time.Time{}, false
	}
	next := strings.TrimSpace(string(b))
	if next == "" || !filepath.IsAbs(next) || sameDir(next, dir) {
		return "", time.Time{}, false
	}
	target, err := os.Stat(next)
	if err != nil || !target.IsDir() {
		return "", time.Time{}, false
	}
	return next, st.ModTime(), true
}

// dataDirTargetOK keeps a recovered pointer from adopting one of the nested
// default directories the old APPDATA bug created.
func dataDirTargetOK(defaultDir, target string) bool {
	if !filepath.IsAbs(target) || sameDir(target, defaultDir) {
		return false
	}
	if withinDir(defaultDir, target) || hasRepeatedBaseName(target) {
		return false
	}
	return true
}

func hasRepeatedBaseName(path string) bool {
	dir := filepath.Clean(path)
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		if strings.EqualFold(filepath.Base(dir), baseName()) &&
			strings.EqualFold(filepath.Base(parent), baseName()) {
			return true
		}
		dir = parent
	}
}

// followDataDirPointers follows a chain earlier versions left behind, without
// letting it lead back into the default directory.
func followDataDirPointers(start, defaultDir string) string {
	seen := map[string]bool{}
	dir := start
	for range 8 {
		if seen[sameKey(dir)] {
			break
		}
		seen[sameKey(dir)] = true
		next, _, ok := readDataDirPointer(dir)
		if !ok || !dataDirTargetOK(defaultDir, next) {
			break
		}
		dir = next
	}
	return dir
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
