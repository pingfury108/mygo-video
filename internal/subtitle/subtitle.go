// Package subtitle reads the subtitles that sit beside a video: SRT and
// WebVTT, found by the video's name.
package subtitle

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mygo-video/internal/executil"
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
		base + ".ass",
		base + ".ssa",
		base + ".zh.ass",
		base + ".zh-CN.ass",
		base + ".chs.ass",
		base + ".zh.ssa",
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

// Parse reads SRT, WebVTT, or the Dialogue lines of ASS and SSA.
func Parse(data []byte) []Cue {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.Contains(text, "[Events]") && strings.Contains(text, "Dialogue:") {
		return parseASS(text)
	}
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

// parseASS reads the Dialogue lines of an ASS or SSA script, taking the
// times and the text and leaving the styling to the reader.
func parseASS(text string) []Cue {
	var cues []Cue
	inEvents := false
	var format []string
	field := func(name string) int {
		for i, f := range format {
			if f == name {
				return i
			}
		}
		return -1
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.EqualFold(line, "[Events]"):
			inEvents = true
			continue
		case strings.HasPrefix(line, "["):
			inEvents = false
			continue
		}
		if !inEvents {
			continue
		}
		if strings.HasPrefix(line, "Format:") {
			format = nil
			for _, f := range strings.Split(strings.TrimPrefix(line, "Format:"), ",") {
				format = append(format, strings.ToLower(strings.TrimSpace(f)))
			}
			continue
		}
		if !strings.HasPrefix(line, "Dialogue:") || len(format) == 0 {
			continue
		}
		// The text is the last field and may hold commas of its own.
		fields := strings.SplitN(strings.TrimPrefix(line, "Dialogue:"), ",", len(format))
		iStart, iEnd, iText := field("start"), field("end"), field("text")
		if iStart < 0 || iEnd < 0 || iText < 0 || iText >= len(fields) {
			continue
		}
		start, okStart := parseTime(fields[iStart])
		end, okEnd := parseTime(fields[iEnd])
		if !okStart || !okEnd {
			continue
		}
		body := fields[iText]
		body = strings.ReplaceAll(body, `\N`, "\n")
		body = strings.ReplaceAll(body, `\n`, "\n")
		body = strings.ReplaceAll(body, `\h`, " ")
		body = strings.TrimSpace(markup.ReplaceAllString(body, ""))
		if body == "" || end <= start {
			continue
		}
		cues = append(cues, Cue{Start: start, End: end, Text: body})
	}
	return cues
}

// Extract asks ffmpeg for the first subtitle track inside a video, when
// there is no file beside it. It returns none if there is no such track or
// no ffmpeg.
func Extract(video, ffmpeg string) []Cue {
	if ffmpeg == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-v", "error", "-i", video,
		"-map", "0:s:0", "-f", "srt", "pipe:1")
	executil.Hide(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil
	}
	return Parse(out.Bytes())
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
