//go:build !windows

package app

import (
	"time"

	"mygo-video/internal/media"
)

// playMPV reports that mpv is wired on Windows alone.
func (a *app) playMPV(it media.Item, start time.Duration) bool { return false }
