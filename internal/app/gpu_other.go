//go:build !windows

package app

import (
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
)

// gpuPlayer reports that the GPU decoder is wired on Windows alone, and the
// player the system gives plays the video.
func (a *app) gpuPlayer(it media.Item) (*ui.VideoPlayer, bool) { return nil, false }
