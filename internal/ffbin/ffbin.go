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
// resources, then the PATH. Empty when there is none.
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
	return ""
}

func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
