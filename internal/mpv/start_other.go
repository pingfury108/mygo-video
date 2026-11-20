//go:build !windows && !darwin

package mpv

import (
	"errors"
	"time"
)

// ErrUnsupported says mpv is not wired on this platform.
var ErrUnsupported = errors.New("mpv is not wired here")

// Start reports that mpv is not wired here.
func Start(bin, url string, from time.Duration, wid uintptr) (*Player, error) {
	return nil, ErrUnsupported
}
