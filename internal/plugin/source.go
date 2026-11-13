package plugin

import "mygo-video/internal/media"

// Home is the plugin's own page.
func (p *Plugin) Home() (*media.Browse, error) {
	var b media.Browse
	if err := p.call("home", nil, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// Browse is a page of the plugin's own, by the name its navigation gives.
func (p *Plugin) Browse(page string) (*media.Browse, error) {
	var b media.Browse
	if err := p.call("browse", map[string]string{"page": page}, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// Search finds videos.
func (p *Plugin) Search(query string, offset int) (*media.SearchResult, error) {
	var r media.SearchResult
	if err := p.call("search", map[string]any{"query": query, "offset": offset}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Title is a video with its episodes.
func (p *Plugin) Title(id string) (*media.TitlePage, error) {
	var t media.TitlePage
	if err := p.call("title", map[string]string{"id": id}, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Play is what to play for a video: a stream the app proxies, or the page of
// the source's own player. A stream under DRM comes back with ErrDRM.
func (p *Plugin) Play(id, quality string) (*media.Playback, error) {
	var pb media.Playback
	if err := p.call("play", map[string]string{"id": id, "quality": quality}, &pb); err != nil {
		return nil, err
	}
	if pb.DRM != "" {
		return &pb, ErrDRM
	}
	if pb.URL == "" && pb.Open == "" {
		return nil, ErrUnavailable
	}
	return &pb, nil
}

// Open is where the source plays a video itself, for one that can't hand
// over a stream.
func (p *Plugin) Open(id string) (string, error) {
	var r struct {
		URL string `json:"url"`
	}
	if err := p.call("open", map[string]string{"id": id}, &r); err != nil {
		return "", err
	}
	return r.URL, nil
}

// Progress tells the plugin where the listener is, for sources that keep it.
func (p *Plugin) Progress(id string, position, duration int64, done bool) error {
	if !p.exports["progress"] {
		return nil
	}
	return p.call("progress", map[string]any{
		"id":       id,
		"position": position,
		"duration": duration,
		"done":     done,
	}, nil)
}

// Account is who is signed in, or nil.
func (p *Plugin) Account() (*Account, error) {
	var a *Account
	if err := p.call("account", nil, &a); err != nil {
		return nil, err
	}
	return a, nil
}

// SignIn signs in.
func (p *Plugin) SignIn() (*Account, error) {
	var a Account
	if err := p.call("signIn", nil, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// SignOut signs out.
func (p *Plugin) SignOut() error {
	return p.call("signOut", nil, nil)
}

// Account is who is signed in to a source.
type Account struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

// SetCookie keeps the cookies the app signed the plugin in with.
func (p *Plugin) SetCookie(header string) {
	p.mu.Lock()
	p.cookie = header
	p.mu.Unlock()
}

// Cookie is what host.cookie returns: the session's cookies, or empty.
func (p *Plugin) Cookie() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cookie
}
