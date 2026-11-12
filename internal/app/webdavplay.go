package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"

	"mygo-video/internal/media"
)

// webdavCacheMost is how much of the server's videos stay on this computer,
// roughly: the oldest go once it is past.
const webdavCacheMost = 10 << 30

// playWebDAV plays a file of the server: one already here at once, else
// after downloading it. A non-faststart MP4 streamed over HTTP stalls the
// system's player, as 4K ones do, so the file comes here first.
func (a *app) playWebDAV(it media.Item, fromStart bool) {
	a.closePlayer()
	path := a.webdavCachePath(it)
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		a.openWebDAVFile(it, path, fromStart)
		return
	}
	a.pruneWebDAVCache()
	ctx, cancel := context.WithCancel(context.Background())
	a.webdavCancel = cancel
	a.webdavPlayItem = it
	a.webdavPlayStart = fromStart
	a.webdavDownloading = true
	a.webdavProgress = 0
	a.playOpening = true
	a.route = pagePlayer
	a.invalidate()
	go a.downloadWebDAV(ctx, it, path)
}

func (a *app) webdavCachePath(it media.Item) string {
	return filepath.Join(a.dataDir, "cache", thumbKey(it)+".mp4")
}

// downloadWebDAV fetches a video to the cache, off the main thread.
func (a *app) downloadWebDAV(ctx context.Context, it media.Item, path string) {
	err := a.downloadTo(ctx, it.RemoteID, path)
	mygo.RunOnMain(func() {
		a.webdavDownloading = false
		a.webdavCancel = nil
		a.playOpening = false
		if err != nil {
			if ctx.Err() == nil {
				a.say("下载失败：%v", err)
				a.route = pageDetail
			}
			a.invalidate()
			return
		}
		a.openWebDAVFile(it, path, a.webdavPlayStart)
	})
}

func (a *app) downloadTo(ctx context.Context, raw, path string) error {
	return a.fetchTo(ctx, raw, path, func(done, total int64) {
		mygo.RunOnMain(func() {
			if total > 0 {
				a.webdavProgress = float64(done) / float64(total)
			}
			a.invalidate()
		})
	})
}

func (a *app) fetchTo(ctx context.Context, raw, path string, progress func(done, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	part := path + ".part"
	var start int64
	if st, err := os.Stat(part); err == nil {
		start = st.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	for k, v := range a.webdavAuth() {
		req.Header.Set(k, v)
	}
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("%s", resp.Status)
	}
	flags := os.O_CREATE | os.O_WRONLY
	if start > 0 && resp.StatusCode == http.StatusPartialContent {
		flags |= os.O_APPEND
	} else {
		start = 0
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return err
	}
	total := resp.ContentLength + start
	done := start
	buf := make([]byte, 256<<10)
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			done += int64(n)
			if progress != nil && time.Since(last) > 500*time.Millisecond {
				last = time.Now()
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(part, path)
}

func (a *app) openWebDAVFile(it media.Item, path string, fromStart bool) {
	p, err := video.Open(path)
	if err != nil {
		a.say("%v", err)
		return
	}
	a.subs = nil
	a.subsOn = false
	a.beginPlayback(it, p, fromStart)
}

// cancelPreparing stops fetching the video, and goes back.
func (a *app) cancelPreparing() {
	if a.webdavCancel != nil {
		a.webdavCancel()
		a.webdavCancel = nil
	}
	a.webdavDownloading = false
	a.playOpening = false
	a.route = pageDetail
	a.invalidate()
}

// preparingPage says how fetching the video goes.
func (a *app) preparingPage(c *ui.Context) {
	p := colors()
	name := a.webdavPlayItem.Name
	c.OnShortcut(0, ui.KeyEscape, a.cancelPreparing)
	ui.Box(c).Fill().Background(ui.Hex("#000000")).Center().Children(func() {
		ui.Column(c).Gap(12).AlignItems(ui.Center).Width(420).Children(func() {
			ui.Text(c, "正在准备 "+name).FontSize(14).FontWeight(600).SingleLine()
			ui.Text(c, "先把视频下载到本机，这样画面和拖动才稳。").
				FontSize(11.5).TextColor(ui.RGBA(255, 255, 255, 0.55))
			ui.Box(c).Height(4).Radius(2).Background(ui.RGBA(255, 255, 255, 0.16)).Children(func() {
				ui.Row(c).Fill().Children(func() {
					ui.Box(c).Grow(float32(a.webdavProgress)).Height(4).Background(p.accent)
					ui.Box(c).Grow(float32(1 - a.webdavProgress))
				})
			})
			ui.Text(c, fmt.Sprintf("%d%%", int(a.webdavProgress*100))).
				FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.6))
			ghostButton(c, "取消", iconBack, a.cancelPreparing)
		})
	})
}

// pruneWebDAVCache drops the oldest downloads past the room they may take.
func (a *app) pruneWebDAVCache() {
	dir := filepath.Join(a.dataDir, "cache")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type download struct {
		path string
		size int64
		mod  time.Time
	}
	var files []download
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, download{filepath.Join(dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	if total <= webdavCacheMost {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= webdavCacheMost {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}
