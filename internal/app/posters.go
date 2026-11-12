package app

import (
	"io"
	"net/http"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/media"
)

// posterKeep is how many plugin posters stay decoded.
const posterKeep = 120

// posterBitmap is a video's poster from its source, or nil while it loads,
// when it can't be reached, or when the plugin may not reach its host.
func (a *app) posterBitmap(it media.Item) *ui.Bitmap {
	if it.Poster == "" {
		return nil
	}
	if it.Source != "" {
		p := a.pluginByID(it.Source)
		if p == nil || !p.AllowsHost(it.Poster) {
			return nil
		}
	}
	if b, ok := a.posters[it.Poster]; ok {
		a.posterUsed[it.Poster] = time.Now()
		return b
	}
	if !a.posterBusy[it.Poster] {
		a.posterBusy[it.Poster] = true
		go a.loadPoster(it.Poster)
	}
	return nil
}

func (a *app) loadPoster(raw string) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(raw)
	if err != nil {
		mygo.RunOnMain(func() {
			delete(a.posterBusy, raw)
			a.invalidate()
		})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		mygo.RunOnMain(func() {
			delete(a.posterBusy, raw)
			a.invalidate()
		})
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		mygo.RunOnMain(func() {
			delete(a.posterBusy, raw)
			a.invalidate()
		})
		return
	}
	mygo.RunOnMain(func() {
		delete(a.posterBusy, raw)
		if b, err := ui.DecodeBitmap(data); err == nil {
			a.posters[raw] = b
			a.posterUsed[raw] = time.Now()
			a.evictPosters()
		}
		a.invalidate()
	})
}

// evictPosters drops the posters not shown for the longest.
func (a *app) evictPosters() {
	for len(a.posters) > posterKeep {
		oldest := ""
		var at time.Time
		for url := range a.posters {
			used := a.posterUsed[url]
			if oldest == "" || used.Before(at) {
				oldest, at = url, used
			}
		}
		if oldest == "" {
			return
		}
		a.posters[oldest].ReleasePixels()
		delete(a.posters, oldest)
		delete(a.posterUsed, oldest)
	}
}
