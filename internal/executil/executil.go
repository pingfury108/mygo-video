//go:build !windows

// Package executil starts the programs the app needs without showing them.
package executil

import "os/exec"

// Hide keeps a console program from opening a window of its own. Outside
// Windows a child process gets no window unless it asks for one, so this
// does nothing there.
func Hide(cmd *exec.Cmd) {}
