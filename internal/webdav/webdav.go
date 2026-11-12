// Package webdav lists a WebDAV server's folders and files, with the
// basic authentication a NAS usually asks for.
package webdav

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Config is where a server is, and who to sign in as.
type Config struct {
	URL  string
	User string
	Pass string
}

// Client lists one server.
type Client struct {
	cfg  Config
	base *url.URL
	http *http.Client
}

// New reads the address, which starts with http or https.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return nil, errors.New("webdav: the address needs http:// or https://")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return &Client{
		cfg:  cfg,
		base: u,
		http: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Entry is a folder or a file on the server.
type Entry struct {
	// Path is where it is, from the server's root, without a leading
	// slash: "Movies/Heat (1995).mkv".
	Path    string
	Name    string
	Dir     bool
	Size    int64
	ModTime time.Time
}

// List is what dir holds, dir itself left out: "" is the root.
func (c *Client) List(ctx context.Context, dir string) ([]Entry, error) {
	target := c.DirURL(dir)
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", target, strings.NewReader(propfindBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	if c.cfg.User != "" || c.cfg.Pass != "" {
		req.SetBasicAuth(c.cfg.User, c.cfg.Pass)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("webdav: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var ms multistatus
	if err := xml.Unmarshal(body, &ms); err != nil {
		return nil, err
	}
	dir = clean(dir)
	var entries []Entry
	for _, r := range ms.Responses {
		rel, ok := c.relative(r.Href)
		if !ok || rel == dir {
			continue
		}
		// A listing may give the children of a folder only; keep the ones
		// directly in dir.
		if dir != "" && !strings.HasPrefix(rel, dir+"/") {
			continue
		}
		if dir == "" && strings.Contains(rel, "/") {
			continue
		}
		e := Entry{Path: rel, Name: path.Base(rel)}
		for _, ps := range r.Propstat {
			if !strings.Contains(ps.Status, "200") {
				continue
			}
			if ps.Prop.DisplayName != "" {
				e.Name = ps.Prop.DisplayName
			}
			e.Dir = ps.Prop.ResourceType.Collection != nil
			e.Size = ps.Prop.ContentLength
			if ps.Prop.LastModified != "" {
				if t, err := http.ParseTime(ps.Prop.LastModified); err == nil {
					e.ModTime = t
				}
			}
			break
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// URL is where a path on the server is.
func (c *Client) URL(p string) string {
	u := *c.base
	rel := strings.Trim(clean(p), "/")
	u.Path = c.base.Path
	if rel == "" {
		u.Path += "/"
		return u.String()
	}
	u.Path += "/" + rel
	return u.String()
}

// DirURL is a folder's address, which ends in a slash.
func (c *Client) DirURL(p string) string {
	s := c.URL(p)
	if !strings.HasSuffix(s, "/") {
		s += "/"
	}
	return s
}

// relative is an href as a path from the base, unless it is elsewhere.
func (c *Client) relative(href string) (string, bool) {
	u, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	p := u.Path
	base := c.base.Path
	if base != "" && base != "/" {
		if !strings.HasPrefix(p, base) {
			return "", false
		}
		p = strings.TrimPrefix(p, base)
	}
	return clean(p), true
}

func clean(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop>
    <d:displayname/>
    <d:resourcetype/>
    <d:getcontentlength/>
    <d:getlastmodified/>
  </d:prop>
</d:propfind>`

type multistatus struct {
	Responses []struct {
		Href     string `xml:"href"`
		Propstat []struct {
			Prop struct {
				DisplayName   string `xml:"displayname"`
				ContentLength int64  `xml:"getcontentlength"`
				LastModified  string `xml:"getlastmodified"`
				ResourceType  struct {
					Collection *struct{} `xml:"collection"`
				} `xml:"resourcetype"`
			} `xml:"prop"`
			Status string `xml:"status"`
		} `xml:"propstat"`
	} `xml:"response"`
}
