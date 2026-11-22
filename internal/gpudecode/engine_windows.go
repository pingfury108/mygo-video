//go:build windows

package gpudecode

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

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
	// seeking says a seek has begun and its call has not returned: a picture
	// decoded meanwhile belongs to neither place, and is passed over.
	seeking bool
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
	gen, seeked, seekUs, seeking := w.gen, w.seeked, w.seekUs, w.seeking
	w.mu.Unlock()
	if seeking {
		w.freeSlot(slot)
		return ui.VideoPicture{}, 0, errStale
	}

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
	stale := w.gen != gen || w.seeking
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
	if w.gen != gen || w.seeking {
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
	w.seeking = true
	w.mu.Unlock()
	err := w.d.Seek(d)
	w.mu.Lock()
	if err == nil {
		// The place is committed only by a seek that landed: one that
		// failed would otherwise measure the next pictures from a place
		// the decoder never reached.
		w.seekUs = d.Microseconds()
		w.seeked = true
	}
	w.seeking = false
	w.mu.Unlock()
	return err
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
func (w winAudio) SetVolume(v float64) int  { return w.a.SetVolume(v) }
func (w winAudio) SetMuted(m bool) int      { return w.a.SetMuted(m) }
func (w winAudio) SetRate(r float64) int    { return w.a.SetRate(r) }

// NewEngine reads url and plays it on the GPU. The read happens apart, so the
// window never waits for the network.
func NewEngine(url string, headers []string) (*Engine, error) {
	e := newEngine(nil, nil)
	// The picture and the sound share one flag that says the engine is
	// closing: a read waiting on the network gives up at once instead of
	// holding the window's close for its timeout.
	flag := newAbortFlag()
	e.mu.Lock()
	e.abortIO = func() { setAbortFlag(flag) }
	e.finishIO = func() { freeAbortFlag(flag) }
	e.mu.Unlock()
	e.wg.Add(1)
	go e.open(url, headers, flag)
	return e, nil
}

// openFailed reports why the file could not be opened, unless the engine is
// closing: a read interrupted by the close is not a failure of the film.
func (e *Engine) openFailed(err error) {
	audioLog(err.Error())
	e.mu.Lock()
	if !e.stopped {
		e.err = err
		e.state = ui.VideoFailed
	}
	e.mu.Unlock()
}

// open reads the video and the sound, and starts both.
func (e *Engine) open(url string, headers []string, abort unsafe.Pointer) {
	defer e.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			audioLog(fmt.Sprintf("open panic: %v", r))
		}
	}()
	audioLog("open: " + url)
	d, err := Open(url, headers, abort)
	if err != nil {
		e.openFailed(err)
		return
	}
	// A close may have come while the file opened. Nothing holds the
	// decoder yet, so it is let go here and Close's wait can finish.
	e.mu.Lock()
	stopped := e.stopped
	e.mu.Unlock()
	if stopped {
		_ = d.Close()
		return
	}
	// The picture and the sound begin where the playhead stands: a seek may
	// have come before either was ready.
	e.mu.Lock()
	startAt := e.at
	rate := e.rate
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
	// A seek can land while the picture opens: the sound starts at the
	// newest place, not the one read before the read.
	e.mu.Lock()
	startAt = e.at
	rate = e.rate
	e.mu.Unlock()
	audioLog("audio opening")
	var snd clock
	if a, aerr := openAudio(url, headers, startAt, rate, abort); aerr == nil {
		snd = winAudio{a}
		audioLog("audio made")
	} else {
		audioLog("audio: " + aerr.Error())
		if errors.Is(aerr, errAudioSeek) {
			// The sound could not follow the picture to the place asked
			// for: playing it from the start would be a different film,
			// so the playback is reported as failed for the app to try
			// its next way in.
			e.openFailed(aerr)
			_ = nf.Close()
			return
		}
	}
	// The place may have moved again while the sound opened. Both are
	// moved to the newest one before either is published, so nothing of
	// the place left behind is shown or heard.
	e.mu.Lock()
	stopped, latest := e.stopped, e.at
	e.mu.Unlock()
	if stopped {
		if snd != nil {
			snd.Close()
		}
		_ = nf.Close()
		return
	}
	if latest != startAt {
		audioLog(fmt.Sprintf("open: moved to %v while opening", latest))
		if err := nf.Seek(latest); err != nil {
			e.openFailed(err)
			if snd != nil {
				snd.Close()
			}
			_ = nf.Close()
			return
		}
		if snd != nil {
			if rc := snd.Seek(latest); rc != 0 {
				e.openFailed(fmt.Errorf("gpudecode: sound seek at step %d", rc))
				snd.Close()
				_ = nf.Close()
				return
			}
		}
		startAt = latest
	}
	e.mu.Lock()
	// A drag that came between the check and now is left to the engine's
	// own seek: it is the same path every later drag takes.
	stopped, moved := e.stopped, e.at
	if !stopped {
		e.src = nf
		e.snd = snd
	}
	wake := e.wake
	e.mu.Unlock()
	if stopped {
		if snd != nil {
			snd.Close()
		}
		_ = nf.Close()
		return
	}
	audioLog("open done")
	if wake != nil {
		wake()
	}
	if snd != nil {
		e.wg.Add(1)
		go e.runSound()
	}
	if moved != startAt {
		e.Seek(moved)
	}
}
