//go:build windows

package app

import (
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/gpudecode"
	"mygo-video/internal/media"
)

// gpuPlayer plays a video with libavcodec's Direct3D 11 decoder, which keeps its
// pictures on the GPU: the system's media framework has no HEVC decoder on some
// machines, and the CPU cannot carry a 4K picture through a transcode beside it.
func (a *app) gpuPlayer(it media.Item) (*ui.VideoPlayer, bool) {
	url := it.Path
	if it.Source == "webdav" {
		url = it.RemoteID
	}
	if url == "" {
		return nil, false
	}
	e, err := gpudecode.NewEngine(url, nil)
	if err != nil {
		a.diag("gpu engine: %v", err)
		return nil, false
	}
	a.diag("gpu engine: %s", url)
	return ui.NewVideoPlayerOf(e), true
}
