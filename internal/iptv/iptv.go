// Package iptv reads M3U playlists of live channels.
package iptv

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Channel is one stream of a playlist.
type Channel struct {
	Name  string `json:"name"`
	Group string `json:"group,omitempty"`
	Logo  string `json:"logo,omitempty"`
	URL   string `json:"url"`
}

// Parse reads an M3U playlist: an #EXTINF line names a channel, and the
// line after it is where that channel streams.
func Parse(r io.Reader) ([]Channel, error) {
	var out []Channel
	seen := map[string]bool{}
	pending := Channel{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "#EXTINF:"):
			pending = parseExtinf(line)
		case strings.HasPrefix(line, "#EXTGRP:"):
			if pending.Group == "" {
				pending.Group = strings.TrimSpace(strings.TrimPrefix(line, "#EXTGRP:"))
			}
		case strings.HasPrefix(line, "#"):
		default:
			ch := pending
			ch.URL = line
			if ch.Name == "" {
				ch.Name = line
			}
			if ch.Group == "" {
				ch.Group = "未分组"
			}
			if !seen[ch.URL] {
				seen[ch.URL] = true
				out = append(out, ch)
			}
			pending = Channel{}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("列表里没有频道")
	}
	return out, nil
}

// parseExtinf reads a line as #EXTINF:-1 tvg-logo="…" group-title="…",name.
func parseExtinf(line string) Channel {
	var ch Channel
	body := strings.TrimPrefix(line, "#EXTINF:")
	if i := strings.LastIndex(body, ","); i >= 0 {
		ch.Name = strings.TrimSpace(body[i+1:])
		body = body[:i]
	}
	ch.Logo = attr(body, "tvg-logo")
	ch.Group = attr(body, "group-title")
	if ch.Name == "" {
		ch.Name = attr(body, "tvg-name")
	}
	return ch
}

// attr is the value of key in an #EXTINF line, quoted or bare.
func attr(s, key string) string {
	i := strings.Index(s, key+"=")
	if i < 0 {
		return ""
	}
	rest := s[i+len(key)+1:]
	if strings.HasPrefix(rest, `"`) {
		rest = rest[1:]
		if j := strings.Index(rest, `"`); j >= 0 {
			return rest[:j]
		}
		return ""
	}
	if j := strings.IndexAny(rest, " \t"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// Load reads a playlist from an http(s) address or a file.
func Load(src string) ([]Channel, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("没有填写列表地址")
	}
	if strings.Contains(src, "://") {
		client := &http.Client{Timeout: 30 * time.Second}
		req, err := http.NewRequest("GET", src, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "MyGo Video")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("列表返回 %s", resp.Status)
		}
		return Parse(io.LimitReader(resp.Body, 10<<20))
	}
	f, err := os.Open(strings.TrimPrefix(src, "file://"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}
