//go:build windows

package gpudecode

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// winFrames adapts the Direct3D 11 decoder to the engine's frames.
// winFrames hands the engine pictures from the decoder, each with how far
// ahead of where the seek or the start put us it shows: the picture's own
// PTS turned into time, which a variable frame rate needs. A generation
// counts the seeks, so a picture decoded for a place the playback has left
// is dropped rather than measured against the new one.
type winFrames struct {
	mu sync.Mutex
	d  *Decoder
	// seekUs is where the last seek put the playhead, and seeked says there
	// was one: a seek to zero is still a seek, and must not be taken for
	// the start of the film.
	seekUs int64
	seeked bool
	gen    int64
	// busy marks the picture slots the engine still holds, and next is the
	// slot after the last one taken: slots come round in turn, so one a
	// picture just left is not filled again until the others have been.
	busy [pictureSlots]bool
	next int
	// held maps a picture's texture handle to the slot it was converted
	// into, for Release.
	held map[uintptr]pictureHold
}

// pictureHold is the slot a picture took, and the surface that says it is
// still the same picture when Release comes: a stale release must not free
// a slot the decoder has since given to another frame.
type pictureHold struct {
	slot    int
	surface any
}

// pictureSlots is how many pictures may stand converted at once. The engine
// queues a few pictures ahead of the sound, and each needs a texture of its
// own; two slots beyond that are kept for a picture the window is still
// reading as the decoder comes round to it.
// It must match CONV_SLOTS in bgra_windows.c.
const pictureSlots = 8

func newWinFrames(d *Decoder) *winFrames {
	return &winFrames{d: d, held: map[uintptr]pictureHold{}}
}

// takeSlot finds a slot for a picture, or reports that every one is held.
func (w *winFrames) takeSlot() (int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := 0; i < pictureSlots; i++ {
		s := (w.next + i) % pictureSlots
		if !w.busy[s] {
			w.busy[s] = true
			w.next = (s + 1) % pictureSlots
			return s, true
		}
	}
	return 0, false
}

// freeSlot gives a slot back, for a picture that failed before the engine
// could hold it.
func (w *winFrames) freeSlot(slot int) {
	w.mu.Lock()
	w.busy[slot] = false
	w.mu.Unlock()
}

// Capacity is how many pictures the engine may queue: the slots less the two
// the window may still be reading.
func (w *winFrames) Capacity() int { return pictureSlots - 2 }

// Release gives back the slot a shown or dropped picture occupied. The
// surface is not changed: the window may still be drawing the picture as
// the decoder comes round to the slot.
func (w *winFrames) Release(p ui.VideoPicture) {
	if p.Surface == nil {
		return
	}
	w.mu.Lock()
	if h, ok := w.held[p.Surface.Native]; ok && h.surface == p.Surface {
		delete(w.held, p.Surface.Native)
		w.busy[h.slot] = false
	}
	w.mu.Unlock()
}

func (w *winFrames) Next() (ui.VideoPicture, time.Duration, error) {
	slot, ok := w.takeSlot()
	if !ok {
		return ui.VideoPicture{}, 0, errNoSlot
	}
	w.mu.Lock()
	gen, seeked, seekUs := w.gen, w.seeked, w.seekUs
	w.mu.Unlock()

	f, err := w.d.Next()
	if err != nil {
		w.freeSlot(slot)
		return ui.VideoPicture{}, 0, err
	}
	if !f.OnGPU {
		w.freeSlot(slot)
		return ui.VideoPicture{}, 0, errors.New("gpudecode: picture not on the GPU")
	}
	// A seek came while this one decoded: its place belongs to where the
	// playback no longer is, and it is not worth a conversion.
	w.mu.Lock()
	stale := w.gen != gen
	w.mu.Unlock()
	if stale {
		w.freeSlot(slot)
		return ui.VideoPicture{}, 0, errStale
	}
	handle, bw, bh, err := w.d.Picture(slot)
	if err != nil {
		// A picture that carries no texture is the one the decoder flushed
		// after a seek, before it decoded a fresh one: it is passed over and
		// the next is decoded, rather than ending the whole playback.
		w.freeSlot(slot)
		return ui.VideoPicture{}, 0, errStale
	}
	// The handle is the key the renderer caches the opened texture by: two
	// videos make different textures, and a key of one number would keep
	// showing the picture of the one played before.
	off := time.Duration(0)
	tb := w.d.Timebase()
	w.mu.Lock()
	if w.gen != gen {
		// A seek came while this one decoded: its place belongs to where the
		// playback no longer is, and its PTS must not anchor the new place.
		w.mu.Unlock()
		w.freeSlot(slot)
		return ui.VideoPicture{}, 0, errStale
	}
	if tb > 0 {
		ptsUs := int64(float64(f.PTS) * tb * 1e6)
		if seeked {
			// A seek landed on the picture the index holds before the place
			// asked for: one before it shows before its time, and the engine
			// passes those over, so both the picture and the sound start
			// from the place the seek asked for.
			off = time.Duration(ptsUs-seekUs) * time.Microsecond
		} else {
			// The film's own timeline, not a timeline made by subtracting
			// the first picture's PTS: the sound is on the film's timeline,
			// and a title whose first picture does not start at zero must
			// stay where its timestamp puts it.
			off = time.Duration(ptsUs) * time.Microsecond
		}
	}
	pic := ui.NewTexturePicture(handle, uint64(handle), bw, bh)
	w.held[handle] = pictureHold{slot: slot, surface: pic.Surface}
	w.mu.Unlock()
	return pic, off, nil
}

func (w *winFrames) Seek(d time.Duration) error {
	w.mu.Lock()
	w.gen++
	w.seekUs = d.Microseconds()
	w.seeked = true
	w.mu.Unlock()
	return w.d.Seek(d)
}

func (w *winFrames) SetSkip(on bool)         { w.d.Skip(on) }
func (w *winFrames) Duration() time.Duration { return w.d.Duration() }
func (w *winFrames) Size() (int, int)        { return w.d.Size() }
func (w *winFrames) Close() error            { return w.d.Close() }
func (w *winFrames) Step() time.Duration {
	if f := w.d.FPS(); f > 0 {
		return time.Duration(float64(time.Second) / f)
	}
	return 40 * time.Millisecond
}

// winAudio adapts WASAPI to the engine's clock.
type winAudio struct{ a *Audio }

func (w winAudio) Position() time.Duration  { return w.a.Position() }
func (w winAudio) Delay() time.Duration     { return w.a.Delay() }
func (w winAudio) Seek(d time.Duration) int { return w.a.Seek(d) }
func (w winAudio) Fill() int                { return w.a.Fill() }
func (w winAudio) Play()                    { w.a.Play() }
func (w winAudio) Pause()                   { w.a.Pause() }
func (w winAudio) Done() bool               { return w.a.Ended() }
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
	nf := newWinFrames(d)
	if startAt > 0 {
		audioLog(fmt.Sprintf("video seek to %v", startAt))
		// The engine already stands at startAt, so the pictures of the seek
		// count from there: without this the first picture's own timestamp
		// would be added to the place again.
		if err := nf.Seek(startAt); err != nil {
			audioLog("video seek: " + err.Error())
		}
	}
	audioLog("audio opening")
	var snd clock
	if a, aerr := openAudio(url, headers, startAt); aerr == nil {
		snd = winAudio{a}
		audioLog("audio made")
	} else {
		audioLog("audio: " + aerr.Error())
	}
	e.mu.Lock()
	e.src = nf
	e.snd = snd
	wake := e.wake
	e.mu.Unlock()
	audioLog("open done")
	if wake != nil {
		wake()
	}
	if snd != nil {
		e.wg.Add(1)
		go e.runSound()
	}
}
