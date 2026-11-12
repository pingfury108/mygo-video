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
	parts := []string{}
	if it.Series != "" {
		parts = append(parts, it.Series)
		if label := it.EpisodeLabel(); label != "" {
			parts = append(parts, label)
		}
	}
	parts = append(parts, strings.ToUpper(strings.TrimPrefix(it.Ext, ".")))
	parts = append(parts, HumanSize(it.Size))
	return strings.Join(parts, " · ")
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
