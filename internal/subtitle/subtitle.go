// Package subtitle reads the subtitles that sit beside a video: SRT and
// WebVTT, found by the video's name.
package subtitle

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Cue is one line of subtitles, or a few, and when it shows.
type Cue struct {
	Start time.Duration
	End   time.Duration
	Text  string
}

// Load finds the subtitles beside video and reads them: <name>.srt,
// <name>.vtt, or one named for a language, as <name>.zh.srt. It returns
// none when there is no file.
func Load(video string) []Cue {
	path := Find(video)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return Parse(data)
}

// Find is the subtitle file of a video, or "".
func Find(video string) string {
	base := strings.TrimSuffix(video, filepath.Ext(video))
	candidates := []string{
		base + ".srt",
		base + ".vtt",
		base + ".zh.srt",
		base + ".zh-CN.srt",
		base + ".chs.srt",
		base + ".chi.srt",
		base + ".en.srt",
		base + ".zh.vtt",
	}
	for _, path := range candidates {
		if st, err := os.Stat(path); err == nil && !st.IsDir() && st.Size() > 0 {
			return path
		}
	}
	return ""
}

// At is the text showing at d, or "".
func At(cues []Cue, d time.Duration) string {
	var parts []string
	for _, c := range cues {
		if d >= c.Start && d <= c.End {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

var markup = regexp.MustCompile(`<[^>]*>|\{\\[^}]*\}`)

// Parse reads SRT or WebVTT cues.
func Parse(data []byte) []Cue {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	var cues []Cue
	var cur *Cue
	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = strings.TrimSpace(markup.ReplaceAllString(cur.Text, ""))
		if cur.Text != "" && cur.End > cur.Start {
			cues = append(cues, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		if strings.Contains(line, "-->") {
			parts := strings.SplitN(line, "-->", 2)
			start, okStart := parseTime(parts[0])
			end, okEnd := parseTime(parts[1])
			if okStart && okEnd {
				flush()
				cur = &Cue{Start: start, End: end}
			}
			continue
		}
		if cur == nil {
			continue // the number above a cue, or WEBVTT
		}
		if cur.Text != "" {
			cur.Text += "\n"
		}
		cur.Text += line
	}
	flush()
	return cues
}

// parseTime reads 00:00:01,000, 00:01.000 or 01:02. Split from anything
// after it, as WebVTT's settings on the same line.
func parseTime(s string) (time.Duration, bool) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return 0, false
	}
	s = strings.ReplaceAll(fields[0], ",", ".")
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	var secs float64
	for i, part := range parts {
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0, false
		}
		if i < len(parts)-1 {
			secs = secs*60 + v
		} else {
			secs = secs*60 + v
		}
	}
	return time.Duration(secs * float64(time.Second)), true
}
