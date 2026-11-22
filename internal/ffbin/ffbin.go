// Package ffbin finds the ffmpeg and ffprobe the app uses: the one the
// listener chose, the one shipped with the app, or one on the PATH.
package ffbin

import (
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// NoProxyArgs keeps an ffmpeg input on the local network from going through
// a proxy in the environment. ffmpeg reads http_proxy for every HTTP input,
// and a proxy meant for the internet turns a WebDAV server beside the
// computer into an unreachable host.
func NoProxyArgs(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	if !localHost(u.Hostname()) {
		return nil
	}
	return []string{"-http_proxy", ""}
}

func localHost(host string) bool {
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") ||
		strings.HasSuffix(strings.ToLower(host), ".local") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	// A name without a dot is resolved on the local network.
	return !strings.Contains(host, ".")
}
