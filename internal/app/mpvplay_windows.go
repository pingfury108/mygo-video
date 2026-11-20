//go:build windows

package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"mygo-video/internal/executil"
	"mygo-video/internal/media"
)

// mpvBin is the mpv beside ffmpeg, which the Windows build ships.
func (a *app) mpvBin() string {
	if a.ffmpeg == "" {
		return ""
	}
	p := filepath.Join(filepath.Dir(a.ffmpeg), "mpv.exe")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// playMPV hands the video to mpv, which decodes it in hardware and keeps its
// sound in step, in a window of its own: the player here decodes the picture
// on the GPU already, but its sound has no such sure clock.
func (a *app) playMPV(it media.Item, start time.Duration) bool {
	bin := a.mpvBin()
	if bin == "" {
		return false
	}
	url := it.Path
	if it.Source == "webdav" {
		url = it.RemoteID
	}
	if url == "" || it.Live {
		return false
	}
	args := []string{"--osc=yes", "--force-window=yes", "--title=" + it.Name}
	if start > 0 {
		args = append(args, "--start="+fmt.Sprintf("%.3f", start.Seconds()))
	}
	if it.Source == "webdav" {
		args = append(args, "--no-config", "--cache=yes")
	}
	args = append(args, url)
	cmd := exec.Command(bin, args...)
	executil.Hide(cmd)
	if err := cmd.Start(); err != nil {
		a.diag("mpv: %v", err)
		return false
	}
	a.diag("mpv: %s", url)
	a.say("已在 mpv 窗口播放")
	go func() {
		_ = cmd.Wait()
	}()
	return true
}
