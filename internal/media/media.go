// Package media is what the app knows of a video: the file it is, what its
// name says it belongs to, and how far the listener watched.
package media

import (
	"fmt"
	"strings"
	"time"
)

// Item is one video file the library lists.
type Item struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Folder  string    `json:"folder,omitempty"`
	Ext     string    `json:"ext"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	// Series, Season and Episode come from the file name, as "Fog Harbour
	// S02E05" has them; they are empty for a film.
	Series  string `json:"series,omitempty"`
	Season  int    `json:"season,omitempty"`
	Episode int    `json:"episode,omitempty"`

	// A video a plugin lists has no file of its own: Source is the plugin's
	// id, RemoteID its id there, and Poster and Overview what it says of it.
	Source   string `json:"source,omitempty"`
	RemoteID string `json:"remoteId,omitempty"`
	URL      string `json:"url,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Overview string `json:"overview,omitempty"`
	Duration int64  `json:"duration,omitempty"` // milliseconds
	// Live says the video is a live channel, which has no length and keeps
	// no place.
	Live bool `json:"live,omitempty"`
}

// EpisodeLabel is "S02E05", or "" for a film.
func (it Item) EpisodeLabel() string {
	if it.Season == 0 && it.Episode == 0 {
		return ""
	}
	return fmt.Sprintf("S%02dE%02d", it.Season, it.Episode)
}

// Subtitle is what a row shows under the name.
func (it Item) Subtitle() string {
	if it.Source == "webdav" {
		var parts []string
		if it.Ext != "" {
			parts = append(parts, strings.ToUpper(strings.TrimPrefix(it.Ext, ".")))
		}
		if it.Size > 0 {
			parts = append(parts, HumanSize(it.Size))
		}
		if len(parts) == 0 {
			return "视频"
		}
		return strings.Join(parts, " · ")
	}
	if it.Source != "" {
		var parts []string
		if it.Series != "" {
			parts = append(parts, it.Series)
		}
		if label := it.EpisodeLabel(); label != "" {
			parts = append(parts, label)
		}
		if it.Duration > 0 {
			parts = append(parts, HumanDuration(it.Duration))
		}
		if len(parts) == 0 {
			// A card without a length says what it does instead, as a
			// catalog entry does.
			if it.Overview != "" {
				return short(it.Overview, 24)
			}
			return ""
		}
		return strings.Join(parts, " · ")
	}
	parts := []string{}
	if label := it.EpisodeLabel(); label != "" {
		parts = append(parts, label)
	}
	parts = append(parts, strings.ToUpper(strings.TrimPrefix(it.Ext, ".")))
	parts = append(parts, HumanSize(it.Size))
	return strings.Join(parts, " · ")
}

// short is s cut to n runes, with an ellipsis when it was longer.
func short(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

// Progress is where the listener stands in an item.
type Progress struct {
	Position int64 `json:"position"`           // milliseconds
	Duration int64 `json:"duration,omitempty"` // milliseconds
	Done     bool  `json:"done,omitempty"`
	Updated  int64 `json:"updated,omitempty"` // milliseconds since 1970
}

// Fraction is how much of it was watched, from 0 to 1.
func (p Progress) Fraction() float64 {
	if p.Duration <= 0 {
		return 0
	}
	f := float64(p.Position) / float64(p.Duration)
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// Started reports whether it was watched enough to offer "continue".
func (p Progress) Started() bool { return !p.Done && p.Position > 2000 }

// HumanSize is a file size as Finder shows it.
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

// HumanDuration is a length as a player shows it: 4:05, or 1:04:05.
func HumanDuration(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	total := ms / 1000
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// A Video is a title a plugin lists: a film, an episode, a clip.
type Video struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Series   string `json:"series,omitempty"`
	Season   int    `json:"season,omitempty"`
	Episode  int    `json:"episode,omitempty"`
	Duration int64  `json:"duration,omitempty"` // milliseconds
	Poster   string `json:"poster,omitempty"`
	Backdrop string `json:"backdrop,omitempty"`
	Overview string `json:"overview,omitempty"`
	// Live says the video is a live channel with no length of its own.
	Live bool `json:"live,omitempty"`
	// URL is the video's own page at its source, for sharing or for a
	// source that plays there instead of here.
	URL string `json:"url,omitempty"`
}

// Section is a row of a plugin's page.
type Section struct {
	Title  string  `json:"title,omitempty"`
	Videos []Video `json:"videos,omitempty"`
}

// Browse is a page a plugin makes up itself: its Home, or one opened from
// its sidebar.
type Browse struct {
	Title    string    `json:"title,omitempty"`
	Subtitle string    `json:"subtitle,omitempty"`
	Sections []Section `json:"sections,omitempty"`
}

// SearchResult is what a plugin's search found.
type SearchResult struct {
	Videos []Video `json:"videos,omitempty"`
	More   bool    `json:"more,omitempty"`
}

// TitlePage is a title and its episodes, for a series.
type TitlePage struct {
	Video    Video   `json:"video"`
	Episodes []Video `json:"episodes,omitempty"`
}

// Playback is what a plugin answers to play a video.
type Playback struct {
	URL     string            `json:"url,omitempty"`
	Type    string            `json:"type,omitempty"` // hls, mp4
	Headers map[string]string `json:"headers,omitempty"`
	// Open is where to play it instead, for a source that only knows where
	// its own player is: the app opens it in the listener's browser.
	Open     string `json:"open,omitempty"`
	Duration int64  `json:"duration,omitempty"` // milliseconds
	Live     bool   `json:"live,omitempty"`
	// DRM says the stream is protected, as "widevine", which the app can't
	// play and doesn't try to bypass.
	DRM string `json:"drm,omitempty"`
	// Quality is what the source called the stream it gave, as "1080".
	Quality string `json:"quality,omitempty"`
	// Dash is a picture and its sound kept apart, which the proxy joins.
	Dash *Dash `json:"dash,omitempty"`
	// Relay is the template every fetch of the stream goes through, as a
	// collection site's own server takes it: %s carries the source's
	// address, escaped. Empty fetches directly.
	Relay string `json:"relay,omitempty"`
}

// Dash is a video and its sound at two addresses of their own.
type Dash struct {
	Video string `json:"video"`
	Audio string `json:"audio,omitempty"`
}

// A Setting is something a plugin asks the listener for.
type Setting struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Help  string `json:"help,omitempty"`
	Link  string `json:"link,omitempty"`
}

// A NavItem is an entry of a plugin's sidebar.
type NavItem struct {
	Title   string `json:"title"`
	Page    string `json:"page,omitempty"`
	Builtin string `json:"builtin,omitempty"`
}

// Info is how a plugin introduces itself.
type Info struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Version    string    `json:"version,omitempty"`
	Playback   string    `json:"playback,omitempty"` // stream or catalog
	Settings   []Setting `json:"settings,omitempty"`
	Navigation []NavItem `json:"navigation,omitempty"`
}
