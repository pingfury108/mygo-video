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

	"mygo-video/internal/media"
)

// Data is what the file holds.
type Data struct {
	Progress map[string]media.Progress `json:"progress"`
	Volume   float64                   `json:"volume"`
	Roots    []string                  `json:"roots,omitempty"`
	WebDAV   WebDAVConfig              `json:"webdav,omitempty"`
	// Items is what the last scan found, shown at once on the next run
	// while a new scan goes on.
	Items     []media.Item `json:"items,omitempty"`
	ScannedAt int64        `json:"scannedAt,omitempty"`
}

// WebDAVConfig is the server a WebDAV source reads.
type WebDAVConfig struct {
	URL  string `json:"url,omitempty"`
	User string `json:"user,omitempty"`
	Pass string `json:"pass,omitempty"`
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
		data: Data{Progress: map[string]media.Progress{}, Volume: 0.8},
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
		s.data = Data{Progress: map[string]media.Progress{}, Volume: 0.8}
		return s, nil
	}
	if s.data.Progress == nil {
		s.data.Progress = map[string]media.Progress{}
	}
	if s.data.Volume <= 0 {
		s.data.Volume = 0.8
	}
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
