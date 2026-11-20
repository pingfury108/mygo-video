package app

import (
	"io"
	"net/http"
	"net/url"
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
		// A poster is a picture the plugin hands the app to fetch, which the
		// stream hosts speak for: collection sites rename their picture
		// hosts too often for a list of their own.
		p := a.pluginByID(it.Source)
		if p == nil || !p.AllowsStreamHost(it.Poster) {
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
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return
	}
	// Some picture hosts refuse a request that names no page, as Douban's
	// answers 418 to one without its own site in the Referer.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
	if u, e := url.Parse(raw); e == nil {
		req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")
	}
	resp, err := client.Do(req)
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
