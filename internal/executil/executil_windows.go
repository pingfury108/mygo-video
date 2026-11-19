//go:build windows

package executil

import (
	"os/exec"
	"syscall"
)

// Hide runs the program without a console window. ffmpeg is a console
// program, so Windows opens one for every frame taken and every remux,
// which flashes over the app.
func Hide(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}
