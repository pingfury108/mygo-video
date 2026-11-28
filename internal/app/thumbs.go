package app

import (
	"context"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/videoplayer"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
	"mygo-video/internal/thumb"
)

// thumbKeep is how many thumbnails stay decoded: enough for the rows and
// grids on screen, without holding every frame of a large library in
// memory.
const thumbKeep = 120

// thumbnail is the frame of a video for its card, or nil while it loads or
// when this machine has no ffmpeg to take one. Cards fall back to the
// placeholder their name gives them.
func (a *app) thumbnail(it media.Item) *ui.Bitmap {
	if it.Path == "" && it.Source != "webdav" {
		return nil
	}
	if b, ok := a.thumbs[it.ID]; ok {
		a.thumbUsed[it.ID] = time.Now()
		return b
	}
	if !a.thumbBusy[it.ID] && (a.ffmpeg != "" || runtime.GOOS == "android") {
		a.thumbBusy[it.ID] = true
		go a.loadThumb(it)
	}
	return nil
}

func (a *app) loadThumb(it media.Item) {
	if a.thumbSem != nil {
		a.thumbSem <- struct{}{}
		defer func() { <-a.thumbSem }()
	}
	key := thumbKey(it)
	path := thumb.Path(a.thumbDir, key)
	switch {
	case it.Source == "webdav":
		if _, err := os.Stat(path); err != nil {
			if err := a.makeThumb(it, path); err != nil {
				log.Printf("thumb %s: %v", it.Name, err)
			}
		}
	default:
		if st, err := os.Stat(path); err != nil || st.ModTime().Before(it.ModTime) {
			if err := a.makeThumb(it, path); err != nil {
				log.Printf("thumb %s: %v", it.Name, err)
			}
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		mygo.RunOnMain(func() {
			delete(a.thumbBusy, it.ID)
			a.invalidate()
		})
		return
	}
	mygo.RunOnMain(func() {
		delete(a.thumbBusy, it.ID)
		if b, err := ui.DecodeBitmap(data); err == nil {
			a.thumbs[it.ID] = b
			a.thumbUsed[it.ID] = time.Now()
			a.evictThumbs()
		}
		a.invalidate()
	})
}

// makeThumb takes a frame with ffmpeg when the machine has it, and with the
// phone system's own frame reader when it does not.
func (a *app) makeThumb(it media.Item, path string) error {
	if runtime.GOOS == "android" {
		var last error
		for _, at := range []time.Duration{10 * time.Second, time.Second} {
			var err error
			if it.Source == "webdav" {
				err = videoplayer.Thumbnail(context.Background(), it.RemoteID, a.webdavAuth(), at, thumb.Width, path)
			} else {
				err = videoplayer.Thumbnail(context.Background(), it.Path, nil, at, thumb.Width, path)
			}
			if err == nil {
				if st, e := os.Stat(path); e == nil && st.Size() > 0 {
					return nil
				}
				err = os.ErrNotExist
			}
			last = err
		}
		return last
	}
	if it.Source == "webdav" {
		return thumb.MakeURL(context.Background(), a.ffmpeg, it.RemoteID, a.webdavAuth(), path)
	}
	return thumb.Make(context.Background(), a.ffmpeg, it.Path, path)
}

// evictThumbs drops the frames not shown for the longest, once there are
// more than thumbKeep.
func (a *app) evictThumbs() {
	for len(a.thumbs) > thumbKeep {
		oldest := ""
		var at time.Time
		for id := range a.thumbs {
			used := a.thumbUsed[id]
			if oldest == "" || used.Before(at) {
				oldest, at = id, used
			}
		}
		if oldest == "" {
			return
		}
		a.thumbs[oldest].ReleasePixels()
		delete(a.thumbs, oldest)
		delete(a.thumbUsed, oldest)
	}
}

// pregenerateThumbs takes the frames missing, a few at a time, so cards find
// them ready. It runs off the main thread.
func (a *app) pregenerateThumbs(items []media.Item) {
	if a.ffmpeg == "" {
		return
	}
	sem := make(chan struct{}, 3)
	for _, it := range items {
		path := thumb.Path(a.thumbDir, it.ID)
		if st, err := os.Stat(path); err == nil && !st.ModTime().Before(it.ModTime) {
			continue
		}
		sem <- struct{}{}
		go func(it media.Item, path string) {
			defer func() { <-sem }()
			if err := thumb.Make(context.Background(), a.ffmpeg, it.Path, path); err != nil {
				log.Printf("thumb %s: %v", it.Name, err)
			}
		}(it, path)
	}
	mygo.RunOnMain(a.invalidate)
}

// thumbKey is an item's thumbnail file name: a WebDAV one has no colon in
// its ID where a file name goes.
func thumbKey(it media.Item) string {
	if it.Source == "webdav" {
		return strings.TrimPrefix(it.ID, "webdav:")
	}
	return it.ID
}
