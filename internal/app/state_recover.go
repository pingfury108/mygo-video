package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"mygo-video/internal/media"
	"mygo-video/internal/store"
)

// recoverNestedState repairs what the old APPDATA bug left behind: a chosen
// directory whose state was copied from one of the empty nested directories
// while the listener's progress stayed in the default one. It only runs when
// bootstrap found the pointer in a nested directory and the chosen state has
// no progress of its own, so it cannot overwrite a directory in use.
func recoverNestedState(dir, defaultDir string) {
	if dir == "" || defaultDir == "" || sameDir(dir, defaultDir) {
		return
	}
	path := filepath.Join(dir, "state.json")
	current, exists := readRecoveredState(path)
	if exists && len(current.Progress) > 0 {
		return
	}
	oldDirs := nestedDataDirs(defaultDir)
	merged := current
	if merged.Progress == nil {
		merged.Progress = map[string]media.Progress{}
	}
	for _, old := range oldDirs {
		if sameDir(old, dir) {
			continue
		}
		donor, ok := readRecoveredState(filepath.Join(old, "state.json"))
		if !ok {
			continue
		}
		mergeRecoveredState(&merged, donor)
	}
	merged.Roots = recoveredRoots(current.Roots, oldDirs, dir)
	if reflect.DeepEqual(merged, current) {
		return
	}
	if exists {
		backup := path + ".before-recover.bak"
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			if b, err := os.ReadFile(path); err == nil {
				_ = os.WriteFile(backup, b, 0o600)
			}
		}
	}
	if b, err := json.MarshalIndent(merged, "", "  "); err == nil {
		writeRecoveredState(path, b)
	}
}

// nestedDataDirs lists the default directory and the chain of "MyGo Video"
// directories the old relaunch bug created.
func nestedDataDirs(defaultDir string) []string {
	var dirs []string
	dir := defaultDir
	for range 16 {
		dirs = append(dirs, dir)
		next := filepath.Join(dir, baseName())
		if st, err := os.Stat(next); err != nil || !st.IsDir() {
			break
		}
		dir = next
	}
	return dirs
}

func readRecoveredState(path string) (store.Data, bool) {
	var data store.Data
	b, err := os.ReadFile(path)
	if err != nil {
		return data, false
	}
	if json.Unmarshal(b, &data) != nil {
		return data, false
	}
	if data.Progress == nil {
		data.Progress = map[string]media.Progress{}
	}
	return data, true
}

// mergeRecoveredState keeps what the chosen directory already has and fills
// what it lost. A newer progress entry wins; settings are taken only when the
// chosen directory left them empty.
func mergeRecoveredState(into *store.Data, from store.Data) {
	for key, p := range from.Progress {
		if old, ok := into.Progress[key]; !ok || p.Updated > old.Updated {
			into.Progress[key] = p
		}
	}
	if into.Volume <= 0 {
		into.Volume = from.Volume
	}
	if into.WebDAV.URL == "" {
		into.WebDAV = from.WebDAV
	}
	if into.IPTV.URL == "" {
		into.IPTV = from.IPTV
	}
	if into.Rate <= 0 {
		into.Rate = from.Rate
	}
	if into.AVOffsetMs == 0 {
		into.AVOffsetMs = from.AVOffsetMs
	}
	if into.Source == "" {
		into.Source = from.Source
	}
	if into.Tools.FFmpeg == "" {
		into.Tools = from.Tools
	}
	if into.Plugins == nil {
		into.Plugins = map[string]store.PluginState{}
	}
	for id, state := range from.Plugins {
		if _, ok := into.Plugins[id]; !ok {
			into.Plugins[id] = state
		}
	}
	if into.Danmaku.Opacity <= 0 {
		into.Danmaku.Opacity = from.Danmaku.Opacity
	}
	if into.Danmaku.Size <= 0 {
		into.Danmaku.Size = from.Danmaku.Size
	}
	if into.Danmaku.Speed <= 0 {
		into.Danmaku.Speed = from.Danmaku.Speed
	}
	if into.Danmaku.Area <= 0 {
		into.Danmaku.Area = from.Danmaku.Area
	}
	if into.Danmaku.Block == "" {
		into.Danmaku.Block = from.Danmaku.Block
	}
	if into.ScannedAt == 0 {
		into.ScannedAt = from.ScannedAt
	}
}

// recoveredRoots moves library roots that lived in the nested default tree
// to the directory the app now uses, leaving the Desktop and other folders.
func recoveredRoots(roots, oldDirs []string, dir string) []string {
	out := append([]string(nil), roots...)
	for i, root := range out {
		// A previous recovery could leave "<data>\MyGo Video\...\videos":
		// the directory is already the app's, so take the inner videos.
		if withinDir(dir, root) {
			if rel, err := filepath.Rel(dir, root); err == nil {
				first, _, _ := strings.Cut(rel, string(filepath.Separator))
				if strings.EqualFold(first, baseName()) {
					out[i] = filepath.Join(dir, "videos")
					continue
				}
			}
		}
		replaced := false
		for _, old := range oldDirs {
			if sameDir(root, filepath.Join(old, "videos")) {
				out[i] = filepath.Join(dir, "videos")
				replaced = true
				break
			}
		}
		if replaced {
			continue
		}
		if old, ok := deepestContainingDir(root, oldDirs); ok {
			if withinDir(old, root) {
				if rel, err := filepath.Rel(old, root); err == nil && rel != "." &&
					!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					out[i] = filepath.Join(dir, rel)
				}
			}
		}
	}
	return out
}

// deepestContainingDir picks the most specific nested directory so a path
// under "...\MyGo Video\MyGo Video\videos" does not become
// "...\MyGo Video\videos" under the new root.
func deepestContainingDir(path string, dirs []string) (string, bool) {
	best := ""
	for _, dir := range dirs {
		if withinDir(dir, path) && len(filepath.Clean(dir)) > len(best) {
			best = filepath.Clean(dir)
		}
	}
	return best, best != ""
}

func sameRoots(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameDir(a[i], b[i]) {
			return false
		}
	}
	return true
}

func writeRecoveredState(path string, b []byte) {
	part := path + ".part"
	if err := os.WriteFile(part, b, 0o600); err != nil {
		return
	}
	if err := os.Rename(part, path); err != nil {
		_ = os.Remove(part)
	}
}
