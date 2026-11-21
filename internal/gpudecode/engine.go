// Package gpudecode plays a video whose picture stays on the GPU: the system's
// media framework has no HEVC decoder on some machines, and a decoder that
// copies each picture through memory cannot carry a 4K one.
//
// The engine here keeps the picture on the sound's clock, as a player does: it
// holds a few pictures ahead, shows the one due at the clock's position, and
// drops the ones the playhead passed. What decodes the pictures and what plays
// the sound are separate, so the same engine runs under a test with neither.
package gpudecode

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// frames is what decodes the pictures, one after another. Next hands over
// the picture and how long after where the seek or the start put us it
// shows — its own PTS in time, which a variable frame rate needs.
type frames interface {
	// Next decodes the next picture, or reports io.EOF after the last, with
	// how long after where the seek or the start put us it shows.
	Next() (ui.VideoPicture, time.Duration, error)
	// Seek goes to d and decodes from there.
	Seek(d time.Duration) error
	// Step is how long a picture of it shows.
	Step() time.Duration
	// Duration is how long the video is, 0 when unknown.
	Duration() time.Duration
	// Size is the picture's size in pixels.
	Size() (int, int)
	// Close releases the decoder.
	Close() error
}

// clock is where the sound stands: the device on Windows, a fake one in tests.
type clock interface {
	// Position is how far it has played.
	Position() time.Duration
	// Seek moves it to d, emptying what the device holds.
	Seek(d time.Duration) int
	// Fill writes what the device plays next, and reports how many frames it
	// wrote; 20ms of waiting is wanted when there were none.
	Fill() int
	Play()
	Pause()
	Close()
}

// Engine plays a video on the sound's clock, for ui.NewVideoPlayerOf.
type Engine struct {
	src frames
	snd clock

	mu      sync.Mutex
	wg      sync.WaitGroup // the decoder and the sound, until they stop
	wake    func()
	queue   []queued
	current ui.VideoPicture
	base    time.Duration

	at       time.Duration // where the playhead stood when playback began
	audioPos time.Duration // where the sound stood, read off the frame
	clock    time.Time     // when playback began, zero while paused
	paused   bool
	rate     float64
	state    ui.VideoState
	err      error
	last     time.Duration // where the picture last shown stood
	ended    bool
	stopped  bool
}

// queued is a picture and where it shows.
type queued struct {
	at  time.Duration
	pic ui.VideoPicture
}

// newEngine makes an engine of a decoder and a clock, which it owns.
// newEngine makes an engine of a decoder and a clock, either of which may be
// nil while the platform part still opens: fill and runSound wait for it.
func newEngine(src frames, snd clock) *Engine {
	e := &Engine{src: src, snd: snd, rate: 1, state: ui.VideoLoading}
	e.wg.Add(1)
	go e.fill()
	if snd != nil {
		e.wg.Add(1)
		go e.runSound()
	}
	return e
}

// runSound plays the sound on a goroutine of its own.
func (e *Engine) runSound() {
	defer e.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			audioLog(fmt.Sprintf("sound panic: %v", r))
		}
	}()
	e.snd.Play()
	for {
		e.mu.Lock()
		stopped, paused := e.stopped, e.paused
		e.mu.Unlock()
		if stopped {
			return
		}
		if paused {
			e.snd.Pause()
			time.Sleep(30 * time.Millisecond)
			continue
		}
		e.snd.Play()
		n := e.snd.Fill()
		if n <= 0 {
			time.Sleep(20 * time.Millisecond)
		} else {
			time.Sleep(5 * time.Millisecond)
		}
		// The position is read here, off the window's frame: the call crosses
		// into WASAPI, and a frame that waited for it would stutter. Only while
		// the device moves: at the end it reports nothing written, and the
		// playhead would fall back to zero instead of marking the end.
		if n > 0 {
			e.mu.Lock()
			e.audioPos = e.snd.Position()
			e.mu.Unlock()
		}
	}
}

// fill decodes ahead of the playhead, waking the window as each picture comes.
func (e *Engine) fill() {
	defer e.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			audioLog(fmt.Sprintf("fill panic: %v", r))
		}
	}()
	for {
		e.mu.Lock()
		if e.stopped {
			e.mu.Unlock()
			return
		}
		src, paused, queueLen, wake := e.src, e.paused, len(e.queue), e.wake
		e.mu.Unlock()
		// Keep about a second of video ready, as deep as the sound's buffer:
		// a shallower queue makes the picture stall before the sound does on
		// a slow source, and the two come apart.
		step := 40 * time.Millisecond
		if src != nil {
			if d := src.Step(); d > 0 {
				step = d
			}
		}
		ahead := int((1100 * time.Millisecond) / step)
		if ahead < 12 {
			ahead = 12
		}
		if queueLen >= ahead || paused || src == nil {
			if wake != nil {
				wake()
				// A window woken faster than the video has frames redraws the same
				// picture over and over, and the work crowds the frame it waits for.
				time.Sleep(step / 2)
			} else {
				time.Sleep(50 * time.Millisecond)
			}
			continue
		}

		pic, off, err := src.Next()
		if errors.Is(err, io.EOF) {
			e.mu.Lock()
			e.ended = true
			e.state = ui.VideoEnded
			e.mu.Unlock()
			return
		}
		if err != nil {
			e.mu.Lock()
			e.err = err
			e.state = ui.VideoFailed
			e.mu.Unlock()
			return
		}
		e.mu.Lock()
		e.queue = append(e.queue, queued{at: e.base + off, pic: pic})
		e.state = ui.VideoReady
		wk := e.wake
		e.mu.Unlock()
		if wk != nil {
			wk()
		}
	}
}

// SetWake gives the function that builds a frame of the window.
func (e *Engine) SetWake(wake func()) {
	e.mu.Lock()
	e.wake = wake
	e.mu.Unlock()
}

// Frame returns the picture due at t, and whether it is another than the one
// last returned. The pictures the playhead passed are dropped, so the decoder
// finds room for the next sooner.
func (e *Engine) Frame(t time.Time) (ui.VideoPicture, bool, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.queue) == 0 {
		return e.current, false, e.current.Surface != nil
	}
	pos := e.posAt(t)
	// Drop every picture the playhead passed, keeping the one showing.
	for len(e.queue) > 1 && e.queue[1].at <= pos {
		e.queue = e.queue[1:]
	}
	if e.queue[0].at <= pos {
		changed := e.current.Surface != e.queue[0].pic.Surface
		e.current = e.queue[0].pic
		e.last = e.queue[0].at
		if len(e.queue) > 1 {
			e.queue = e.queue[1:]
		}
		return e.current, changed, true
	}
	if e.current.Surface == nil {
		e.current = e.queue[0].pic
		e.last = e.queue[0].at
		return e.current, true, true
	}
	return e.current, false, true
}

// posAt is where the playhead stands: the sound's position, which is the
// clock, even before it moves. The wall clock only stands in where no sound
// plays, as a video without a track.
func (e *Engine) posAt(t time.Time) time.Duration {
	pos := e.at
	if e.paused {
		return pos
	}
	if e.snd != nil {
		// Before the device moves, its position is 0: the picture waits there
		// for it, rather than running ahead on the wall clock.
		return pos + e.audioPos
	}
	return pos + time.Duration(float64(t.Sub(e.clock))*e.rate)
}

// Interval is 0: each picture wakes the window as it comes due, which keeps the
// picture on the sound's clock rather than on a tick of its own.
func (e *Engine) Interval() time.Duration { return 0 }

// State reports how loading and playing go.
func (e *Engine) State() (ui.VideoState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state, e.err
}

// Size is the picture's size in pixels.
func (e *Engine) Size() (int, int) {
	if e.src == nil {
		return 0, 0
	}
	return e.src.Size()
}

// Duration is how long the video is.
func (e *Engine) Duration() time.Duration {
	if e.src == nil {
		return 0
	}
	return e.src.Duration()
}

// Position is where the playhead stands.
func (e *Engine) Position() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.posAt(time.Now())
}

// Play starts or resumes the video.
func (e *Engine) Play() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.clock.IsZero() || e.paused {
		e.clock = time.Now()
	}
	e.paused = false
}

// Pause stops the video where it stands.
func (e *Engine) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.paused && !e.clock.IsZero() {
		e.at += time.Duration(float64(time.Since(e.clock)) * e.rate)
	}
	e.paused = true
}

// Seek goes to d, restarting the decoder and the sound there.
func (e *Engine) Seek(d time.Duration) {
	e.mu.Lock()
	e.queue = nil
	e.at = d
	e.base = d
	e.last = d
	e.clock = time.Now()
	e.ended = false
	e.state = ui.VideoLoading
	e.audioPos = 0
	e.current = ui.VideoPicture{}
	src, snd := e.src, e.snd
	e.mu.Unlock()
	if snd != nil {
		snd.Seek(d)
	}
	if src != nil {
		go func() {
			_ = src.Seek(d)
			e.mu.Lock()
			e.state = ui.VideoReady
			e.mu.Unlock()
		}()
	}
}

// SetLoop is not wired yet.
func (e *Engine) SetLoop(v bool) {}

// SetRate changes how fast the video plays.
func (e *Engine) SetRate(r float64) {
	e.mu.Lock()
	e.rate = r
	e.mu.Unlock()
}

// SetVolume is not wired yet.
func (e *Engine) SetVolume(v float64) {}

// SetMuted is not wired yet.
func (e *Engine) SetMuted(v bool) {}

// Close ends the decoder and the sound. It waits for the threads that play
// them: a device freed while its thread still fills it is a crash.
func (e *Engine) Close() error {
	e.mu.Lock()
	e.stopped = true
	src, snd := e.src, e.snd
	e.mu.Unlock()
	e.wg.Wait()
	if snd != nil {
		snd.Close()
	}
	if src != nil {
		return src.Close()
	}
	return nil
}

// Stats is where the video and the sound stand.
type Stats struct {
	Audio time.Duration
	Video time.Duration
	Real  time.Duration
	Queue int
	State ui.VideoState
	Err   error
}

// Stat reports how the playback stands, for a check of the sync.
func (e *Engine) Stat(now time.Time) Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := Stats{Queue: len(e.queue), State: e.state, Err: e.err}
	if e.snd != nil {
		s.Audio = e.snd.Position()
	}
	if !e.clock.IsZero() {
		s.Real = now.Sub(e.clock)
	}
	if e.current.Surface != nil {
		// Both stand where the playhead began, as the sound does: the sound of a
		// device counts from its start, not from the file's.
		s.Video = e.last - e.at
	}
	return s
}

// audioLog writes a line for the log of the sound and the playback, where a
// launch that plays none leaves what it got to.
func audioLog(msg string) {
	p := filepath.Join(os.TempDir(), "mygo-audio.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05.000"), msg)
}

// stepOf is how long a picture of the video shows, from the decoder.
func (e *Engine) stepOf() time.Duration {
	e.mu.Lock()
	src := e.src
	e.mu.Unlock()
	if src == nil {
		return 40 * time.Millisecond
	}
	if d := src.Step(); d > 0 {
		return d
	}
	return 40 * time.Millisecond
}
