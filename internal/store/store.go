// Package store keeps what outlives a run: where the listener stopped in each
// video, the volume, and the folders the library scans. It is a JSON file,
// replaced whole on each save, so a crash leaves the last one.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"mygo-video/internal/media"
)

// Data is what the file holds.
type Data struct {
	Progress map[string]media.Progress `json:"progress"`
	Volume   float64                   `json:"volume"`
	Roots    []string                  `json:"roots,omitempty"`
	WebDAV   WebDAVConfig              `json:"webdav,omitempty"`
	IPTV     IPTVConfig                `json:"iptv,omitempty"`
	// Rate is the speed last chosen, for the next video too.
	Rate float64 `json:"rate,omitempty"`
	// AVOffsetMs moves the picture against the sound, in milliseconds: a
	// positive value shows the picture earlier, which a sound that runs ahead
	// of it wants. It belongs to the machine's output, not to a video, so one
	// value serves every playback.
	AVOffsetMs int64 `json:"avOffsetMs,omitempty"`
	// Items is what the last scan found, shown at once on the next run
	// while a new scan goes on.
	Items     []media.Item `json:"items,omitempty"`
	ScannedAt int64        `json:"scannedAt,omitempty"`
	// Source is the one last showing, as "local", "webdav", "iptv" or
	// "plugin:<id>", so enabling a plugin doesn't change what it names.
	Source  SourceID               `json:"source,omitempty"`
	Plugins map[string]PluginState `json:"plugins,omitempty"`
	Tools   Tools                  `json:"tools,omitempty"`
	Danmaku DanmakuPrefs           `json:"danmaku,omitempty"`
}

// DanmakuPrefs is how the comments look: how big, how solid, how fast, and
// how much of the picture they cover. Block holds words to drop, comma
// separated.
type DanmakuPrefs struct {
	Opacity float64 `json:"opacity,omitempty"`
	Size    float64 `json:"size,omitempty"`
	Speed   float64 `json:"speed,omitempty"`
	Area    float64 `json:"area,omitempty"`
	Block   string  `json:"block,omitempty"`
}

// normalize fills in what an older file or a fresh one leaves out.
func (d *DanmakuPrefs) normalize() {
	if d.Opacity <= 0 {
		d.Opacity = 0.92
	}
	if d.Size <= 0 {
		d.Size = 14
	}
	if d.Speed <= 0 {
		d.Speed = 9
	}
	if d.Area <= 0 {
		d.Area = 0.5
	}
}

// WithDefaults is normalize for a value not read from the file yet.
func (d DanmakuPrefs) WithDefaults() DanmakuPrefs {
	d.normalize()
	return d
}

// Tools are the programs the listener named.
type Tools struct {
	FFmpeg string `json:"ffmpeg,omitempty"`
}

// SourceID is the source last showing. Files from before plugins could be
// switched off kept its index, which UnmarshalJSON reads.
type SourceID string

func (s SourceID) MarshalJSON() ([]byte, error) { return json.Marshal(string(s)) }

func (s *SourceID) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = SourceID(v)
		return nil
	}
	var i int
	if err := json.Unmarshal(b, &i); err != nil {
		return err
	}
	switch i {
	case 1:
		*s = "webdav"
	case 2:
		*s = "iptv"
	default:
		*s = "local"
	}
	return nil
}

// PluginState is what a run keeps of a plugin: whether it is on, and when
// it was installed and switched on.
type PluginState struct {
	Enabled     bool   `json:"enabled,omitempty"`
	EnabledAt   int64  `json:"enabledAt,omitempty"`
	InstalledAt int64  `json:"installedAt,omitempty"`
	Origin      string `json:"origin,omitempty"`
}

// WebDAVConfig is the server a WebDAV source reads.
type WebDAVConfig struct {
	URL  string `json:"url,omitempty"`
	User string `json:"user,omitempty"`
	Pass string `json:"pass,omitempty"`
}

// IPTVConfig is the playlist a live source reads.
type IPTVConfig struct {
	URL string `json:"url,omitempty"`
}

// Store is the data in memory, written to path.
type Store struct {
	mu   sync.Mutex
	path string
	data Data
}

// Open reads the store in dir, making a new one if there is none.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		path: filepath.Join(dir, "state.json"),
		data: Data{Progress: map[string]media.Progress{}, Volume: 0.8, Plugins: map[string]PluginState{}},
	}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		// A broken file starts over rather than keeping the app from opening.
		s.data = Data{Progress: map[string]media.Progress{}, Volume: 0.8, Plugins: map[string]PluginState{}}
		return s, nil
	}
	if s.data.Progress == nil {
		s.data.Progress = map[string]media.Progress{}
	}
	if s.data.Volume <= 0 {
		s.data.Volume = 0.8
	}
	if s.data.Rate <= 0 {
		s.data.Rate = 1
	}
	if s.data.Plugins == nil {
		s.data.Plugins = map[string]PluginState{}
	}
	s.data.Danmaku.normalize()
	return s, nil
}

// Volume is the player's volume, from 0 to 1.
func (s *Store) Volume() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Volume
}

// SetVolume keeps the volume.
func (s *Store) SetVolume(v float64) {
	s.mu.Lock()
	s.data.Volume = v
	s.mu.Unlock()
	s.Save()
}

// AVOffset is how far the picture stands from the sound, kept for the next
// playback and the next run.
func (s *Store) AVOffset() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Duration(s.data.AVOffsetMs) * time.Millisecond
}

// SetAVOffset keeps where the picture stands from the sound.
func (s *Store) SetAVOffset(d time.Duration) {
	s.mu.Lock()
	s.data.AVOffsetMs = d.Milliseconds()
	s.mu.Unlock()
	s.Save()
}

// Roots are the folders the library scans, in order.
func (s *Store) Roots() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.data.Roots...)
}

// SetRoots keeps the folders the library scans.
func (s *Store) SetRoots(roots []string) {
	s.mu.Lock()
	s.data.Roots = append([]string(nil), roots...)
	s.mu.Unlock()
	s.Save()
}

// Source is the source last showing.
func (s *Store) Source() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.data.Source)
}

// SetSource keeps the source showing.
func (s *Store) SetSource(id string) {
	s.mu.Lock()
	s.data.Source = SourceID(id)
	s.mu.Unlock()
	s.Save()
}

// Plugins is what a run keeps of the plugins installed.
func (s *Store) Plugins() map[string]PluginState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]PluginState, len(s.data.Plugins))
	for id, st := range s.data.Plugins {
		out[id] = st
	}
	return out
}

// SetPluginEnabled keeps whether a plugin is on.
func (s *Store) SetPluginEnabled(id string, on bool) {
	s.mu.Lock()
	if s.data.Plugins == nil {
		s.data.Plugins = map[string]PluginState{}
	}
	st := s.data.Plugins[id]
	st.Enabled = on
	if on {
		st.EnabledAt = time.Now().Unix()
	}
	s.data.Plugins[id] = st
	s.mu.Unlock()
	s.Save()
}

// SetPluginInstalled keeps when and how a plugin came.
func (s *Store) SetPluginInstalled(id, origin string) {
	s.mu.Lock()
	if s.data.Plugins == nil {
		s.data.Plugins = map[string]PluginState{}
	}
	st := s.data.Plugins[id]
	st.InstalledAt = time.Now().Unix()
	st.Origin = origin
	s.data.Plugins[id] = st
	s.mu.Unlock()
	s.Save()
}

// ForgetPlugin drops what was kept of a plugin.
func (s *Store) ForgetPlugin(id string) {
	s.mu.Lock()
	delete(s.data.Plugins, id)
	s.mu.Unlock()
	s.Save()
}

// WebDAV is the server the WebDAV source reads.
func (s *Store) WebDAV() WebDAVConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.WebDAV
}

// SetWebDAV keeps the WebDAV server.
func (s *Store) SetWebDAV(cfg WebDAVConfig) {
	s.mu.Lock()
	s.data.WebDAV = cfg
	s.mu.Unlock()
	s.Save()
}

// IPTV is the playlist the live source reads.
func (s *Store) IPTV() IPTVConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.IPTV
}

// SetIPTV keeps the playlist.
func (s *Store) SetIPTV(cfg IPTVConfig) {
	s.mu.Lock()
	s.data.IPTV = cfg
	s.mu.Unlock()
	s.Save()
}

// Rate is the speed the player runs at.
func (s *Store) Rate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Rate <= 0 {
		return 1
	}
	return s.data.Rate
}

// SetRate keeps the speed.
func (s *Store) SetRate(r float64) {
	s.mu.Lock()
	s.data.Rate = r
	s.mu.Unlock()
	s.Save()
}

// Tools are the programs the listener named.
func (s *Store) Tools() Tools {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Tools
}

// Danmaku is how the comments look.
func (s *Store) Danmaku() DanmakuPrefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.data.Danmaku
	d.normalize()
	return d
}

// SetDanmaku keeps how the comments look.
func (s *Store) SetDanmaku(d DanmakuPrefs) {
	d.normalize()
	s.mu.Lock()
	s.data.Danmaku = d
	s.mu.Unlock()
	s.Save()
}

// SetTool keeps where ffmpeg is.
func (s *Store) SetTool(name, path string) {
	s.mu.Lock()
	s.data.Tools.FFmpeg = path
	s.mu.Unlock()
	s.Save()
}

// Progress is where the listener stopped in an item.
func (s *Store) Progress(id string) (media.Progress, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data.Progress[id]
	return p, ok
}

// Items is what the last scan found.
func (s *Store) Items() []media.Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]media.Item(nil), s.data.Items...)
}

// ScannedAt is when that scan ended.
func (s *Store) ScannedAt() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.ScannedAt
}

// SetItems keeps what a scan found, so the next run opens on it.
func (s *Store) SetItems(items []media.Item, at int64) {
	const most = 5000
	if len(items) > most {
		items = items[:most]
	}
	s.mu.Lock()
	s.data.Items = items
	s.data.ScannedAt = at
	s.mu.Unlock()
	s.Save()
}

// SetProgress keeps where the listener stopped.
func (s *Store) SetProgress(id string, p media.Progress) {
	s.mu.Lock()
	s.data.Progress[id] = p
	s.mu.Unlock()
	s.Save()
}

// Forget drops what was kept of an item.
func (s *Store) Forget(id string) {
	s.mu.Lock()
	delete(s.data.Progress, id)
	s.mu.Unlock()
	s.Save()
}

// Save writes the data, replacing the file whole.
func (s *Store) Save() {
	s.mu.Lock()
	b, err := json.MarshalIndent(s.data, "", "  ")
	path := s.path
	s.mu.Unlock()
	if err != nil {
		return
	}
	part := path + ".part"
	if err := os.WriteFile(part, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(part, path)
}
