package app

import (
	"errors"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/videoplayer"
	"github.com/egoist/mygo/ui"
)

// isPhone is the app's one shell on phones and tablets. iOS builds get the
// same pages; Android is what ships here.
func isPhone() bool { return runtime.GOOS == "android" || runtime.GOOS == "ios" }

// mobilePlayback wraps MyGo's native player. Its state arrives asynchronously,
// and on Android calls made before the first "ready" are dropped, so a seek
// or a play asked for early waits here until the player can take it.
type mobilePlayback struct {
	p     *videoplayer.Player
	onDid func()

	mu          sync.Mutex
	state       videoplayer.State
	wantPlay    bool
	seek        time.Duration
	hasSeek     bool
	rate        float64
	volume      float64
	panscan     float64
	needRate    bool
	needVolume  bool
	needPanscan bool
	wasReady    bool
	lastErrText string
}

func newMobilePlayback(win *mygo.Window, source string, panscan float64, onDid func()) (*mobilePlayback, error) {
	p, err := videoplayer.New(win, source, videoplayer.Options{Panscan: panscan})
	if err != nil {
		return nil, err
	}
	m := &mobilePlayback{p: p, onDid: onDid, rate: 1, volume: 1, panscan: panscan}
	p.OnState(func(s videoplayer.State) {
		m.mu.Lock()
		m.state = s
		ready := s.Ready
		first := ready && !m.wasReady
		m.wasReady = ready
		seek, hasSeek := m.seek, m.hasSeek
		rate, needRate := m.rate, m.needRate
		volume, needVolume := m.volume, m.needVolume
		panscan, needPanscan := m.panscan, m.needPanscan
		play := m.wantPlay
		if first {
			m.hasSeek = false
			m.needRate = false
			m.needVolume = false
			m.needPanscan = false
		}
		if s.Error != "" {
			m.lastErrText = s.Error
		}
		m.mu.Unlock()
		if first {
			if hasSeek {
				m.p.Seek(seek)
			}
			if needRate {
				m.p.SetRate(rate)
			}
			if needVolume {
				m.p.SetVolume(volume)
			}
			if needPanscan {
				m.p.SetPanscan(panscan)
			}
			if play {
				m.p.Play()
			}
		}
		if m.onDid != nil {
			m.onDid()
		}
	})
	return m, nil
}

func (m *mobilePlayback) View(c *ui.Context) ui.Element {
	return ui.WebView(c, m.p)
}

func (m *mobilePlayback) State() videoplayer.State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *mobilePlayback) Ready() bool   { return m.State().Ready }
func (m *mobilePlayback) Playing() bool { return m.State().Playing }
func (m *mobilePlayback) Ended() bool   { return m.State().Ended }

func (m *mobilePlayback) Buffering() bool { return m.State().Buffering }

func (m *mobilePlayback) Position() time.Duration { return m.State().Position }
func (m *mobilePlayback) Duration() time.Duration { return m.State().Duration }

func (m *mobilePlayback) Err() error {
	s := m.State()
	if s.Error == "" {
		return nil
	}
	return errors.New(s.Error)
}

func (m *mobilePlayback) Play() {
	m.mu.Lock()
	m.wantPlay = true
	ready := m.state.Ready
	m.mu.Unlock()
	if ready {
		m.p.Play()
	}
}

func (m *mobilePlayback) Pause() {
	m.mu.Lock()
	m.wantPlay = false
	ready := m.state.Ready
	m.mu.Unlock()
	if ready {
		m.p.Pause()
	}
}

func (m *mobilePlayback) Seek(to time.Duration) {
	if to < 0 {
		to = 0
	}
	m.mu.Lock()
	m.seek, m.hasSeek = to, true
	ready := m.state.Ready
	m.mu.Unlock()
	if ready {
		m.p.Seek(to)
	}
}

func (m *mobilePlayback) SetRate(r float64) {
	if r <= 0 {
		r = 1
	}
	m.mu.Lock()
	m.rate, m.needRate = r, true
	ready := m.state.Ready
	m.mu.Unlock()
	if ready {
		m.p.SetRate(r)
	}
}

func (m *mobilePlayback) SetVolume(v float64) {
	m.mu.Lock()
	m.volume, m.needVolume = v, true
	ready := m.state.Ready
	m.mu.Unlock()
	if ready {
		m.p.SetVolume(v)
	}
}

func (m *mobilePlayback) SetPanscan(v float64) {
	m.mu.Lock()
	m.panscan, m.needPanscan = v, true
	ready := m.state.Ready
	m.mu.Unlock()
	if ready {
		m.p.SetPanscan(v)
	}
}

func (m *mobilePlayback) Close() {
	m.p.Destroy()
}

// The app reads the player through these, so the desktop engine and the
// phone's native one share the progress, seek and end-of-video logic.
func (a *app) hasPlayer() bool { return a.player != nil || a.mobile != nil }

func (a *app) playerReady() bool {
	if a.mobile != nil {
		return a.mobile.Ready()
	}
	return a.player != nil && a.player.Ready()
}

func (a *app) playerPlaying() bool {
	if a.mobile != nil {
		return a.mobile.Playing()
	}
	return a.player != nil && a.player.Playing()
}

func (a *app) playerEnded() bool {
	if a.mobile != nil {
		return a.mobile.Ended()
	}
	return a.player != nil && a.player.Ended()
}

func (a *app) playerPosition() time.Duration {
	if a.mobile != nil {
		return a.mobile.Position()
	}
	if a.player != nil {
		return a.player.Position()
	}
	return 0
}

func (a *app) playerDuration() time.Duration {
	if a.mobile != nil {
		return a.mobile.Duration()
	}
	if a.player != nil {
		return a.player.Duration()
	}
	return 0
}

func (a *app) playerError() error {
	if a.mobile != nil {
		return a.mobile.Err()
	}
	if a.player != nil {
		return a.player.Err()
	}
	return nil
}
