// Package ffbin finds the ffmpeg and ffprobe the app uses: the one the
// listener chose, the one shipped with the app, or one on the PATH.
package ffbin

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Name is the file a program has on this platform.
func Name(program string) string {
	if runtime.GOOS == "windows" {
		return program + ".exe"
	}
	return program
}

// Locate is where program is: chosen when it exists, the app's own
// resources, the PATH, then the places a package puts it that a GUI app's
// PATH misses.
func Locate(program, chosen, dir string) string {
	if chosen != "" && isFile(chosen) {
		return chosen
	}
	name := Name(program)
	if dir != "" && isFile(filepath.Join(dir, name)) {
		return filepath.Join(dir, name)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, d := range extraDirs {
		p := filepath.Join(d, name)
		if isFile(p) {
			return p
		}
	}
	return ""
}

// extraDirs are where Homebrew, MacPorts and a distribution's packages put
// a program, which the PATH of an app started from the Finder lacks.
var extraDirs = func() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"}
	case "linux":
		return []string{"/usr/local/bin", "/usr/bin", "/snap/bin", "/var/lib/flatpak/exports/bin"}
	}
	return nil
}()

func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
