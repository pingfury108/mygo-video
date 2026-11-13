package app

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"encoding/json"

	"mygo-video/internal/media"
	"mygo-video/internal/webdav"
)

// A WebDAV server is slow to list, and slower to walk: what it said is kept
// on disk, so opening the source shows it at once, while what went stale is
// listed again in the background.
const (
	webdavDirTTL    = 5 * time.Minute
	webdavIndexTTL  = 30 * time.Minute
	webdavDirsMost  = 200
	webdavEntryMost = 5000
)

// webdavCache is what the WebDAV source remembers between runs.
type webdavCache struct {
	URL       string               `json:"url,omitempty"`
	ScannedAt int64                `json:"scannedAt,omitempty"`
	Items     []media.Item         `json:"items,omitempty"`
	Dirs      map[string]webdavDir `json:"dirs,omitempty"`
}

// webdavDir is what a folder held, and when it said so.
type webdavDir struct {
	At      int64          `json:"at"`
	Entries []webdav.Entry `json:"entries"`
}

func (a *app) webdavCacheFile() string {
	return filepath.Join(a.dataDir, "webdav-cache.json")
}

// loadWebDAVCache reads what the last run saw, if it is the same server.
func (a *app) loadWebDAVCache() {
	cfg := a.store.WebDAV()
	if cfg.URL == "" {
		return
	}
	b, err := os.ReadFile(a.webdavCacheFile())
	if err != nil {
		return
	}
	var c webdavCache
	if err := json.Unmarshal(b, &c); err != nil || c.URL != cfg.URL {
		return
	}
	a.webdavDirs = c.Dirs
	if a.webdavDirs == nil {
		a.webdavDirs = map[string]webdavDir{}
	}
	a.webdavAllItems = c.Items
	// A walk kept is what the source opens on, rather than a folder nobody
	// asked to see.
	a.webdavAll = len(c.Items) > 0
	if c.ScannedAt > 0 {
		a.webdavIndexAt = time.UnixMilli(c.ScannedAt)
	}
	if d, ok := c.Dirs[""]; ok {
		a.applyWebDAVDir("", d.Entries, false)
	}
}

// saveWebDAVCache keeps what it has, cutting the folders kept to a few.
func (a *app) saveWebDAVCache() {
	cfg := a.store.WebDAV()
	if cfg.URL == "" {
		return
	}
	dirs := a.webdavDirs
	if len(dirs) > webdavDirsMost {
		type aged struct {
			path string
			at   int64
		}
		list := make([]aged, 0, len(dirs))
		for p, d := range dirs {
			if p == "" || p == a.webdavPath {
				continue
			}
			list = append(list, aged{p, d.At})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].at < list[j].at })
		for i := 0; i < len(list) && len(dirs) > webdavDirsMost; i++ {
			delete(dirs, list[i].path)
		}
	}
	total := 0
	for p, d := range dirs {
		if total+len(d.Entries) > webdavEntryMost {
			delete(dirs, p)
			continue
		}
		total += len(d.Entries)
	}
	c := webdavCache{
		URL:       cfg.URL,
		ScannedAt: a.webdavIndexAt.UnixMilli(),
		Items:     a.webdavAllItems,
		Dirs:      dirs,
	}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	path := a.webdavCacheFile()
	if err := os.WriteFile(path+".part", b, 0o600); err != nil {
		return
	}
	_ = os.Rename(path+".part", path)
}

// applyWebDAVDir shows what a folder held, when it is the one showing,
// unless it is shown already and nothing new came.
func (a *app) applyWebDAVDir(dir string, entries []webdav.Entry, force bool) {
	if a.webdavPath != dir {
		return
	}
	if !force && a.webdavHasLoaded && a.webdavLoaded == dir {
		return
	}
	a.webdavEntries = entries
	a.webdavVideos = a.webdavItemsFrom(entries)
	a.webdavLoaded = dir
	a.webdavHasLoaded = true
}
