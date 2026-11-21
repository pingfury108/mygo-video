//go:build windows

package gpudecode

import (
	"errors"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// winFrames adapts the Direct3D 11 decoder to the engine's frames.
// winFrames hands the engine pictures from the decoder, each with how far
// ahead of where the seek or the start put us it shows: the picture's own
// PTS turned into time, which a variable frame rate needs.
type winFrames struct {
	d      *Decoder
	anchor *int64 // the PTS the offsets count from, in microseconds
}

func (w *winFrames) Next() (ui.VideoPicture, time.Duration, error) {
	f, err := w.d.Next()
	if err != nil {
		return ui.VideoPicture{}, 0, err
	}
	if !f.OnGPU {
		return ui.VideoPicture{}, 0, errors.New("gpudecode: picture not on the GPU")
	}
	handle, bw, bh, err := w.d.Picture()
	if err != nil {
		return ui.VideoPicture{}, 0, err
	}
	// The handle is the key the renderer caches the opened texture by: two
	// videos make different textures, and a key of one number would keep
	// showing the picture of the one played before.
	off := time.Duration(0)
	tb := w.d.Timebase()
	if tb > 0 {
		ptsUs := int64(float64(f.PTS) * tb * 1e6)
		if w.anchor == nil {
			w.anchor = &ptsUs
		}
		off = time.Duration(ptsUs-*w.anchor) * time.Microsecond
	}
	return ui.NewTexturePicture(handle, uint64(handle), bw, bh), off, nil
}

func (w *winFrames) Seek(d time.Duration) error {
	w.anchor = nil
	return w.d.Seek(d)
}
func (w winFrames) Duration() time.Duration { return w.d.Duration() }
func (w winFrames) Size() (int, int)        { return w.d.Size() }
func (w winFrames) Close() error            { return w.d.Close() }
func (w winFrames) Step() time.Duration {
	if f := w.d.FPS(); f > 0 {
		return time.Duration(float64(time.Second) / f)
	}
	return 40 * time.Millisecond
}

// winAudio adapts WASAPI to the engine's clock.
type winAudio struct{ a *Audio }

func (w winAudio) Position() time.Duration  { return w.a.Position() }
func (w winAudio) Seek(d time.Duration) int { return w.a.Seek(d) }
func (w winAudio) Fill() int                { return w.a.Fill() }
func (w winAudio) Play()                    { w.a.Play() }
func (w winAudio) Pause()                   { w.a.Pause() }
func (w winAudio) Close()                   { w.a.Close() }

// NewEngine reads url and plays it on the GPU. The read happens apart, so the
// window never waits for the network.
func NewEngine(url string, headers []string) (*Engine, error) {
	e := newEngine(nil, nil)
	go e.open(url, headers)
	return e, nil
}

// open reads the video and the sound, and starts both.
func (e *Engine) open(url string, headers []string) {
	defer func() {
		if r := recover(); r != nil {
			audioLog(fmt.Sprintf("open panic: %v", r))
		}
	}()
	audioLog("open: " + url)
	d, err := Open(url, headers)
	if err != nil {
		e.mu.Lock()
		e.err = err
		e.state = ui.VideoFailed
		e.mu.Unlock()
		audioLog("video: " + err.Error())
		return
	}
	// The picture and the sound begin where the playhead stands: a seek may
	// have come before either was ready.
	e.mu.Lock()
	startAt := e.at
	e.mu.Unlock()
	if startAt > 0 {
		if err := d.Seek(startAt); err != nil {
			audioLog("video seek: " + err.Error())
		}
	}
	var snd clock
	if a, aerr := openAudio(url, headers, startAt); aerr == nil {
		snd = winAudio{a}
	} else {
		audioLog("audio: " + aerr.Error())
	}
	e.mu.Lock()
	e.src = &winFrames{d: d}
	e.snd = snd
	wake := e.wake
	e.mu.Unlock()
	if wake != nil {
		wake()
	}
	if snd != nil {
		e.wg.Add(1)
		go e.runSound()
	}
}
