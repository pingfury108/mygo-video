// Package local is the built-in source: video files in folders on this
// computer. It scans them on a goroutine and hands the views a snapshot.
package local

import (
	"crypto/md5"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"mygo-video/internal/media"
)

// Extensions are the files the library lists. What actually plays is the
// system's: MP4 and MOV everywhere, the rest where its codecs are.
var Extensions = map[string]bool{
	".mp4": true, ".m4v": true, ".mov": true,
	".mkv": true, ".webm": true, ".avi": true,
}

// maxDepth keeps a scan of a home folder from walking a whole drive.
const maxDepth = 6

// Library is the files found in Roots.
type Library struct {
	mu       sync.RWMutex
	roots    []string
	items    []media.Item
	byID     map[string]media.Item
	scanning bool
	scanned  time.Time
}

// New makes a library of roots, not scanned yet.
func New(roots []string) *Library {
	l := &Library{byID: map[string]media.Item{}}
	l.SetRoots(roots)
	return l
}

// SetRoots says which folders to scan.
func (l *Library) SetRoots(roots []string) {
	l.mu.Lock()
	l.roots = append([]string(nil), roots...)
	l.mu.Unlock()
}

// Roots are the folders it scans.
func (l *Library) Roots() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]string(nil), l.roots...)
}

// Scanning reports whether a scan is under way.
func (l *Library) Scanning() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.scanning
}

// Scanned is when the last scan ended.
func (l *Library) Scanned() time.Time {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.scanned
}

// Scan walks the roots and replaces what it has. Call it off the main thread.
func (l *Library) Scan() int {
	l.mu.Lock()
	l.scanning = true
	roots := append([]string(nil), l.roots...)
	l.mu.Unlock()

	var items []media.Item
	seen := map[string]bool{}
	for _, root := range roots {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path == root {
					return nil
				}
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" || name == "Library" {
					return filepath.SkipDir
				}
				if strings.Count(strings.TrimPrefix(path, root), string(os.PathSeparator)) >= maxDepth {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if !Extensions[ext] {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.Size() < 1024 {
				return nil
			}
			if seen[path] {
				return nil
			}
			seen[path] = true
			items = append(items, itemFor(path, root, ext, info))
			return nil
		})
	}

	sort.Slice(items, func(i, j int) bool { return items[i].ModTime.After(items[j].ModTime) })
	byID := make(map[string]media.Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}

	l.mu.Lock()
	l.items = items
	l.byID = byID
	l.scanning = false
	l.scanned = time.Now()
	l.mu.Unlock()
	return len(items)
}

// SetCached shows what a previous run found, until the next scan replaces
// it. The slice is shared: a scan replaces the whole of it, never its items.
func (l *Library) SetCached(items []media.Item, at time.Time) {
	byID := make(map[string]media.Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	l.mu.Lock()
	l.items = items
	l.byID = byID
	l.scanned = at
	l.mu.Unlock()
}

// Items is every file, newest first.
func (l *Library) Items() []media.Item {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.items
}

// Get finds an item by its ID.
func (l *Library) Get(id string) (media.Item, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	it, ok := l.byID[id]
	return it, ok
}

// Count is how many files it lists.
func (l *Library) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.items)
}

var junk = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|4k|8k|uhd|bluray|blu-ray|brrip|bdrip|web-?dl|web-?rip|hdtv|hdcam|h\.?264|h\.?265|hevc|x264|x265|avc|aac\d?|ac3|dts|truehd|hdr10?|dolby|remux|proper|repack|extended|dubbed|subbed)\b`)

var seriesPattern = regexp.MustCompile(`(?i)(.+?)[\s._-]+s(\d{1,2})[\s._-]*e(\d{1,3})`)

// crossPattern reads "Show 1x05", and the others the ways Chinese sites and
// releases name an episode without a season.
var (
	crossPattern = regexp.MustCompile(`(?i)(.+?)[\s._-]+(\d{1,2})x(\d{1,3})`)
	epPattern    = regexp.MustCompile(`(?i)(.+?)[\s._-]+(?:ep?)[\s._-]?(\d{1,3})`)
	cnPattern    = regexp.MustCompile(`(.+?)[\s._-]*第\s*(\d{1,3})\s*[集话]`)
)

// ParseName reads a file name: its title, and the season and episode it
// says, as "Fog Harbour S02E05", "Show 1x05", "Show E05" or "雾港 第05集".
func ParseName(base string) (title string, season, episode int) {
	name := cleanName(base)
	if m := seriesPattern.FindStringSubmatch(name); m != nil {
		return strings.TrimSpace(m[1]), atoi(m[2]), atoi(m[3])
	}
	if m := crossPattern.FindStringSubmatch(name); m != nil {
		return strings.TrimSpace(m[1]), atoi(m[2]), atoi(m[3])
	}
	if m := cnPattern.FindStringSubmatch(name); m != nil {
		return strings.TrimSpace(m[1]), 0, atoi(m[2])
	}
	if m := epPattern.FindStringSubmatch(name); m != nil {
		return strings.TrimSpace(m[1]), 0, atoi(m[2])
	}
	return name, 0, 0
}

func itemFor(path, root, ext string, info fs.FileInfo) media.Item {
	title, season, episode := ParseName(filepath.Base(path))
	series := ""
	if season != 0 || episode != 0 {
		series = title
	}
	sum := md5.Sum([]byte(path))
	return media.Item{
		ID:      hex.EncodeToString(sum[:])[:12],
		Name:    title,
		Path:    path,
		Folder:  root,
		Ext:     ext,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Series:  series,
		Season:  season,
		Episode: episode,
	}
}

func cleanName(base string) string {
	name := strings.TrimSuffix(base, filepath.Ext(base))
	name = strings.NewReplacer(".", " ", "_", " ", "[", " ", "]", " ", "(", " ", ")", " ").Replace(name)
	name = junk.ReplaceAllString(name, " ")
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return name
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
