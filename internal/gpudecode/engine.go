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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// errStale says a picture was decoded for a place the playback has since
// left: a seek came while it was decoding, and its place would be measured
// against the new one, which puts it seconds away from where it belongs.
var errStale = errors.New("gpudecode: stale picture")

// errNoSlot says every picture slot is in use: the pictures queued ahead of
// the sound have not been shown or dropped yet, so there is nowhere to
// convert the next one until they are.
var errNoSlot = errors.New("gpudecode: every picture slot is in use")

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
	// SetSkip asks the decoder to pass over the pictures that are not
	// reference frames, which a playhead far behind the sound uses to catch
	// up fast.
	SetSkip(on bool)
	// Release gives back the slot a picture occupied, once the engine has
	// shown it or dropped it. A decoder that converts several pictures
	// ahead needs one texture per picture, and those textures are reused
	// only here.
	Release(p ui.VideoPicture)
	// Close releases the decoder.
	Close() error
}

// clock is where the sound stands: the device on Windows, a fake one in tests.
type clock interface {
	// Position is how far it has played.
	Position() time.Duration
	// Delay is how long the sound it holds will take to play, which is
	// how far the picture on screen may lead the sound's own moment.
	Delay() time.Duration
	// Seek moves it to d, emptying what the device holds.
	Seek(d time.Duration) int
	// Fill writes what the device plays next, and reports how many frames it
	// wrote; 20ms of waiting is wanted when there were none.
	Fill() int
	Play()
	Pause()
	// Done says the sound of the file is over, which the picture may
	// outlast: it goes on by the wall clock, rather than freezing at the
	// last of the sound.
	Done() bool
	Close()
}

// Engine plays a video on the sound's clock, for ui.NewVideoPlayerOf.
type Engine struct {
	src frames
	snd clock

	mu sync.Mutex
	wg sync.WaitGroup // the decoder and the sound, until they stop
	// seekWG is the seeks whose decoder call has not returned: the decoder
	// is not freed until they have, and the platform's close flag makes
	// them return.
	seekWG sync.WaitGroup
	// abortIO and finishIO are the platform's way of interrupting the reads
	// that wait on the network, and of giving back the flag.
	abortIO  func()
	finishIO func()
	wake     func()
	queue    []queued
	current  ui.VideoPicture
	base     time.Duration

	at       time.Duration // where the playhead stood when playback began
	audioPos time.Duration // where the sound stood, read off the frame
	// audioAt is when the device last reported, which the playhead is
	// carried from by the wall clock: the report is whole periods apart.
	audioAt time.Time
	// audioStep is how far apart the device's reports come, kept as it
	// reports: the playhead is carried on the wall clock no further than
	// that, for a device that stopped reporting stopped playing too.
	audioStep time.Duration
	// reportAt is when the device last reported, which the step of its
	// reports is read off, kept apart from audioAt, the place the playhead
	// is carried from: a report that repeats a place moves neither, and the
	// step is of the reports, not of the places they name.
	reportAt time.Time
	clock    time.Time // when playback began, zero while paused
	paused   bool
	rate     float64
	volume   float64
	muted    bool
	// sndSeek is a seek for the thread that owns the device to make: the
	// call reads the source and must not hold the window.
	sndSeek *time.Duration
	// sndPending tells the seek the sound's thread was given has not been
	// made yet. Until it is, the device still reports the place before it,
	// and a playhead taking that report would stand a whole playback length
	// past the place asked for — which the picture, left that far behind,
	// jumps to catch up with.
	sndPending bool
	// starting is set from a seek until a picture of the new place is ready:
	// the sound seeks at a frame and the picture at a keyframe it decodes on
	// from, so the sound would be away first and heard alone for as long as
	// the keyframe takes — mpv holds its audio for that (audio.c:
	// audio_start_ao's delaying_audio_start).
	starting bool
	// reanchor tells the place the device reports next is to be taken as it
	// is, however far from the playhead it lies. The device counts from the
	// moment it runs, and the sound of a start is stopped while the picture
	// decodes: its place goes back to zero, and a playhead reading that as
	// the device falling behind steps itself back by the whole of the wait —
	// a start that jumps, and the picture with it.
	reanchor bool
	// seekSeq numbers the seeks: a drag sends many, and only the last is
	// worth the decoder's while, as mpv merges its seeks.
	seekSeq int64
	// seekLanded is the seek whose decoder call has returned. fill waits on
	// it after the end of a film: a seek moves the decoder off its end, but
	// not before the call has reached it.
	seekLanded int64
	// stall counts the fills in a row that wrote nothing, for a source that
	// went quiet: past a minute the two stand together again where the film
	// is, as mpv resets its audio for an underrun.
	stall int
	// presentLag is what a present took to reach the screen, measured by the
	// window and told as each frame is asked for, smoothed gently: the lead
	// follows it, for the compositor's part of a picture's way is in no
	// clock of the engine's.
	presentLag time.Duration
	// syncLoggedAt is when the sync was last written down, once a second.
	syncLoggedAt time.Time
	// frameAt and frameStep are when the window last asked for a picture and
	// how often it does, which is the wait a picture takes to go up.
	frameAt   time.Time
	frameStep time.Duration
	// avShift is the offset the offset file last held, and avReadAt when it
	// was read.
	avShift  time.Duration
	avReadAt time.Time
	avFixed  bool // set by SetAVOffset, which the file no longer overrides
	// step is how long a picture of the video shows, kept from the decoder
	// for the lead a shown picture holds the screen for.
	step     time.Duration
	sndDelay time.Duration
	// traceOn says a file asks for every picture to be written down, and
	// traceAt when that was last checked; traceLast is the last written.
	traceOn   bool
	traceAt   time.Time
	traceLast time.Duration
	state     ui.VideoState
	err       error
	last      time.Duration // where the picture last shown stood
	ended     bool
	stopped   bool
	skipping  bool // the decoder is passing non-reference frames to catch up
	// audioEndedAt is when the sound ran out, zero while it plays: the
	// picture goes on by the wall clock from there, for a film whose
	// closing titles outlast its last sound.
	audioEndedAt time.Time

	// shownVideo and shownAudio are where the picture and the sound stood
	// when the picture now showing went up, read together.
	shownVideo, shownAudio time.Duration
	shown                  bool
	// returned is the picture last handed to the window. A jump empties the
	// queue and changes the picture showing, and the window still has to be
	// told it is another picture even with no queue entry to compare with.
	returned ui.VideoPicture
}

// queued is a picture and where it shows.
type queued struct {
	at  time.Duration
	pic ui.VideoPicture
}

// newEngine makes an engine of a decoder and a clock, either of which may be
// nil while the platform part still opens: fill and runSound wait for it.
func newEngine(src frames, snd clock) *Engine {
	e := &Engine{src: src, snd: snd, rate: 1, volume: 1, state: ui.VideoLoading}
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
	// The sound's calls reach WASAPI, whose apartment belongs to a thread:
	// the thread is pinned and given one of its own here.
	lockSoundThread()
	// The app's level and mute go to the device once, and then only when
	// they change, from this thread that owns the sound.
	lastVolume, lastMuted := -1.0, false
	lastRate := -1.0
	// The sound waits for the picture to have something to show: started
	// first, it ran through the first frames of the film while the decoder
	// was still filling up, and a playback began a third of a second apart.
	for i := 0; i < 100; i++ {
		e.mu.Lock()
		n, stopped := len(e.queue), e.stopped
		e.mu.Unlock()
		if n >= 3 || stopped {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.snd.Play()
	startWait := 0
	for {
		e.mu.Lock()
		stopped, paused := e.stopped, e.paused
		volume, muted := e.volume, e.muted
		rate := e.rate
		seekReq := e.sndSeek
		e.sndSeek = nil
		// The sound waits for a picture of the place it was just sent to:
		// its own seek lands on a frame, the picture's on a keyframe.
		waiting := e.starting && len(e.queue) == 0
		e.mu.Unlock()
		if stopped {
			return
		}
		if volume != lastVolume || muted != lastMuted {
			if vc, ok := e.snd.(interface {
				SetVolume(float64) int
				SetMuted(bool) int
			}); ok {
				vc.SetVolume(volume)
				vc.SetMuted(muted)
			}
			lastVolume, lastMuted = volume, muted
		}
		if rate != lastRate {
			if rc, ok := e.snd.(interface{ SetRate(float64) int }); ok {
				rc.SetRate(rate)
			}
			lastRate = rate
		}
		// The sound is sought here, on its own thread: the seek reads the
		// source over the network, and a window waiting for that would stop
		// drawing for as long as it took.
		if seekReq != nil {
			rc := e.snd.Seek(*seekReq)
			e.mu.Lock()
			if rc != 0 && !e.stopped {
				// The picture was moved and the sound was not: the two
				// would stand apart for the rest of the film, so the retry
				// is asked for at once.
				e.err = fmt.Errorf("gpudecode: sound seek at step %d", rc)
				e.state = ui.VideoFailed
			}
			e.sndPending = false
			e.mu.Unlock()
			continue
		}
		// The sound waits for a picture of the place it was just sent to:
		// its own seek lands on a frame, the picture's on a keyframe, and
		// the keyframe of a 4K HEVC film takes the better part of a second
		// to decode — heard alone, which is what a drag left behind every
		// time. mpv holds its audio start for the same (audio.c:
		// audio_start_ao).
		if waiting {
			e.snd.Pause()
			// A place with no picture at all is a decoder that failed on it:
			// the sound goes on rather than waiting there for good.
			if startWait++; startWait == 400 {
				e.mu.Lock()
				e.starting = false
				e.mu.Unlock()
			}
			time.Sleep(5 * time.Millisecond)
			continue
		}
		startWait = 0
		if paused {
			e.snd.Pause()
			time.Sleep(30 * time.Millisecond)
			continue
		}
		e.snd.Play()
		n := e.snd.Fill()
		if n < 0 {
			// The sound failed in the device, not merely ran out for a
			// moment: playing on would leave a silent picture that looks
			// in step while nothing is heard. The app takes its next way
			// in, as it does for a picture that failed.
			e.mu.Lock()
			if !e.stopped && e.state != ui.VideoFailed {
				e.err = fmt.Errorf("gpudecode: sound at step %d", n)
				e.state = ui.VideoFailed
			}
			e.mu.Unlock()
			return
		}
		if n <= 0 {
			// Nothing was written. A device whose buffer is still full is
			// one the writing waits on, not one that starved: its fills
			// write nothing while the sound it holds plays. The buffer is
			// read here, outside the lock, for the same reason the place is.
			held := e.snd.Delay()
			// A device that drives the writing gave its own wait already.
			time.Sleep(5 * time.Millisecond)
			// A source that went quiet for a long stretch stalls the sound the
			// device still holds, and the picture runs on past it; both stand
			// again where the film is once it comes back, as a seek makes them.
			// mpv resets its audio for the same (audio.c: reset_audio_state on
			// underrun).
			e.mu.Lock()
			// Only a buffer with room left that the source cannot fill is a
			// stall: a full one is the sound waiting its turn, and a start
			// fills it before the device runs. Counting those stood the
			// playhead back on itself — a start that jumped.
			if !e.stopped && !e.starting && held < 150*time.Millisecond {
				e.stall++
				if e.stall%20 == 0 {
					frameLog(fmt.Sprintf("stall %d held %v audioPos %.3f", e.stall, held, e.audioPos.Seconds()))
				}
				if e.stall > 120 { // a minute of failed fills
					e.stall = 0
					e.at = e.posLocked(time.Now())
					e.audioPos = 0
					e.audioAt = time.Time{}
					e.starting = true
					if snd := e.snd; snd != nil {
						d := e.at
						e.sndSeek = &d
						e.sndPending = true
					}
				}
			} else if n == 0 && !e.snd.Done() && !e.stopped {
				// playing, not at the end, nothing written
				if e.stall > 0 {
					e.stall--
				}
			}
			e.mu.Unlock()
		}
		// The position is read here, off the window's frame: the call crosses
		// into WASAPI, and a frame that waited for it would stutter. Only while
		// the device moves: at the end it reports nothing written, and the
		// playhead would fall back to zero instead of marking the end.
		if n > 0 {
			// The call crosses into WASAPI, so it is made outside the lock:
			// a device that waits for its buffer, or one that is being
			// switched, would otherwise hold the engine with it, and the
			// window would stop drawing frames.
			p := e.snd.Position()
			d := e.snd.Delay()
			e.mu.Lock()
			// Not once the playback is paused: the place it stopped at is the
			// last the device reported, and a read that lands after the pause
			// would move the picture on past where it stands.
			if !e.paused && !e.sndPending && !e.starting {
				now := time.Now()
				if e.reanchor {
					frameLog(fmt.Sprintf("reanchor p %.3f audioPos %.3f", p.Seconds(), e.audioPos.Seconds()))
				}
				if e.reanchor && p > 0 {
					// The sound has just started where the picture is: the
					// device counts from there, and this is where it stands.
					// A device that has not moved yet says nothing of where
					// it is, and anchoring the playhead at that zero runs it
					// ahead on the wall clock until the first report, which
					// then looks like the sound falling back.
					e.audioPos = p
					e.audioAt = now
					e.reanchor = false
				} else if p > e.audioPos {
					// The sound moves on and never back. A device naming a
					// place behind the one it named before is settling as it
					// runs, or reading a place from before a seek landed, and
					// following it steps the playhead and the picture back
					// with it: the wall clock carries the playhead until the
					// device is past again.
					e.audioPos = p
					e.audioAt = now
				}
				// How far apart the reports come, which is how far the playhead
				// may be carried past the last of them: kept whether or not
				// this one moved it, for a device that repeats a place while it
				// runs still reports at its own pace.
				if !e.reportAt.IsZero() {
					if d := now.Sub(e.reportAt); d > 0 && d < 200*time.Millisecond {
						if e.audioStep == 0 {
							e.audioStep = d
						} else {
							e.audioStep = (e.audioStep*7 + d) / 8
						}
					}
				}
				e.reportAt = now
				// The sound the device holds is what the pictures already
				// handed over stand against.
				e.sndDelay = d
			}
			e.mu.Unlock()
		}
		// The sound ran out, the picture has not: note the moment, and the
		// playhead carries the picture on by the wall clock from there.
		if e.snd.Done() {
			e.mu.Lock()
			if e.audioEndedAt.IsZero() {
				e.audioEndedAt = time.Now()
			}
			e.mu.Unlock()
		}
	}
}

const (
	// lagSkip is how far the playhead may fall behind the newest ready
	// picture before the decoder passes over the frames that are not
	// reference ones, to catch up at many times the speed of decoding
	// every one of them. lagJump is where a jump to the newest picture
	// beats catching up frame by frame.
	lagSkip = 600 * time.Millisecond
	lagJump = 2500 * time.Millisecond
)

// fill keeps the queue stocked and the decoder's skip on the beat of the
// playhead: behind by over half a second it passes the pictures that are
// not reference ones, over two and a half seconds it jumps, and caught up
// it decodes everything again.
func (e *Engine) fill() {
	defer e.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			audioLog(fmt.Sprintf("fill panic: %v", r))
		}
	}()
	for {
		e.mu.Lock()
		if e.stopped || e.state == ui.VideoFailed {
			e.mu.Unlock()
			return
		}
		src, paused, starting, queueLen, seekSeq := e.src, e.paused, e.starting, len(e.queue), e.seekSeq
		e.mu.Unlock()
		// Keep the pictures the decoder can hold ready. The GPU path converts
		// each one into a texture of its own, and has only a few slots; a
		// source slow enough to starve them still drops frames against the
		// sound below, rather than letting the picture drift away from it.
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
		// A decoder that converts each picture into a texture of its own
		// holds only so many: past them there is nowhere for the next
		// picture to go until the window has shown one.
		if c, ok := src.(interface{ Capacity() int }); ok {
			if n := c.Capacity(); n > 0 && ahead > n {
				ahead = n
			}
		}
		// A seek while paused still fills the first picture of the new
		// place: a player shows it there as the bar is let go, as mpv does,
		// rather than holding the picture of the place left behind until
		// the play goes on.
		if queueLen >= ahead || (paused && !starting) || src == nil {
			// This is the worker that keeps the queue full, not the scheduler:
			// the window takes a picture off the top each of its frames, and the
			// queue has to be refilled as it goes. Sleeping long here left it
			// empty between sleeps, and the picture froze until the next burst
			// — a film shown one second at a time.
			time.Sleep(step / 2)
			continue
		}

		pic, off, err := src.Next()
		if errors.Is(err, errStale) {
			// A seek came while this one decoded: its place belongs to where
			// the playback no longer is.
			continue
		}
		if errors.Is(err, errNoSlot) {
			// Every picture slot is still shown or queued: wait for the
			// window to release one, rather than filling anything else.
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if errors.Is(err, io.EOF) {
			e.mu.Lock()
			e.ended = true
			e.state = ui.VideoEnded
			seq := e.seekSeq
			e.mu.Unlock()
			// The decoder stands at the end, not gone: a seek moves it
			// back into the film, and the pictures go on from there. It is
			// waited for rather than returned from, as mpv keeps its
			// demuxer after the end and seeks it when the film plays
			// again.
			for {
				e.mu.Lock()
				stopped := e.stopped
				moved := e.seekSeq != seq && e.seekLanded >= e.seekSeq
				e.mu.Unlock()
				if stopped {
					return
				}
				if moved {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			continue
		}
		if err != nil {
			e.mu.Lock()
			// A close that interrupts a read is not a failure of the film,
			// and a seek that already named its own error is the one worth
			// keeping.
			if !e.stopped && e.state != ui.VideoFailed {
				e.err = err
				e.state = ui.VideoFailed
			}
			e.mu.Unlock()
			return
		}
		e.mu.Lock()
		// A seek came while this picture decoded: it belongs to the place
		// the playback has left, even where the decoder has not been sent
		// there yet. The seek's own pictures come after it.
		if seekSeq != e.seekSeq {
			e.mu.Unlock()
			releasePictures(src, pic)
			continue
		}
		// A seek lands before the place asked for, and the pictures of that
		// place belong to where the playhead no longer is: they are decoded
		// past, not shown, so the picture starts where the seek asked for, as
		// the sound does.
		if off < 0 {
			e.mu.Unlock()
			releasePictures(src, pic)
			continue
		}
		// The playhead ran past everything ready by a wide margin: a jump
		// stands on the newest picture now, rather than flashing through
		// every one of the ones it passed.
		lag := e.posLocked(time.Now()) - (e.base + off)
		if lag > lagJump {
			oldQueue, oldCurrent := e.queue, e.current
			e.queue = nil
			e.current = pic
			e.last = e.base + off
			e.mu.Unlock()
			pics := make([]ui.VideoPicture, 0, len(oldQueue)+1)
			for _, q := range oldQueue {
				pics = append(pics, q.pic)
			}
			if oldCurrent.Surface != nil {
				pics = append(pics, oldCurrent)
			}
			releasePictures(src, pics...)
			frameLog(fmt.Sprintf("jump picture %.3f lag %v (pos %.3f)", e.last.Seconds(), lag, e.last.Seconds()+lag.Seconds()))
			e.wakeNow()
			// Stand on this picture for its step: without the pause the loop
			// spins for the lock and starves the window and the sound.
			time.Sleep(step)
			continue
		}
		skip := lag > lagSkip
		if e.skipping && lag < lagSkip/2 {
			skip = false // a margin, so the switch does not flicker at the edge
		}
		if skip != e.skipping {
			e.skipping = skip
			e.mu.Unlock()
			src.SetSkip(skip)
			e.mu.Lock()
		}
		e.queue = append(e.queue, queued{at: e.base + off, pic: pic})
		e.state = ui.VideoReady
		e.step = step
		// A picture of the new place is ready, and the sound may start
		// there with it: the device is about to run, and its first report is
		// the place it starts counting from. Only the picture that ends the
		// wait asks for that: every picture after it would ask again, and a
		// report taken as it comes every frame is the playhead following the
		// device's own settling from step to step.
		if e.starting {
			e.starting = false
			e.reanchor = true
		}
		wk := e.wake
		e.mu.Unlock()
		if wk != nil {
			wk()
		}
	}
}

// releasePictures gives back the slots of pictures the engine will not show
// again. A picture may stand in the queue and as the one showing at once, so
// the same texture is given back once.
func releasePictures(src frames, pics ...ui.VideoPicture) {
	if src == nil {
		return
	}
	seen := make(map[uintptr]bool, len(pics))
	for _, p := range pics {
		if p.Surface == nil || p.Surface.Native == 0 {
			continue
		}
		if seen[p.Surface.Native] {
			continue
		}
		seen[p.Surface.Native] = true
		src.Release(p)
	}
}

// wakeNow is wake on a thread that may not hold the lock.
func (e *Engine) wakeNow() {
	e.mu.Lock()
	wake := e.wake
	e.mu.Unlock()
	if wake != nil {
		wake()
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
	// Written outside the lock, after the picture is chosen: read before it,
	// the line compared this moment's playhead with the last one's picture
	// and showed a gap that only the log itself had.
	var pos time.Duration
	var pictureLine string
	defer func() {
		line := ""
		if e.syncLoggedAt.IsZero() || t.Sub(e.syncLoggedAt) >= 250*time.Millisecond {
			e.syncLoggedAt = t
			line = e.syncLine(pos) + " | " + e.presentLagLine()
		}
		// Every call the window makes, not just the calls that bring a new
		// picture: the times between them are the window's own rhythm, which
		// decides when a picture can reach the screen at all. The queue and
		// the sound's place come along, for the one call that changes a
		// picture it had no business changing.
		if e.tracing() {
			fresh := "same"
			if e.last != e.traceLast {
				e.traceLast = e.last
				fresh = "new"
			}
			pictureLine = fmt.Sprintf("%s picture %.3f pos %.3f q=%d audioPos %.3f left %v delay %v", fresh, e.last.Seconds(), pos.Seconds(), len(e.queue), e.audioPos.Seconds(), e.leadLocked().Milliseconds(), e.sndDelay.Milliseconds())
		}
		e.mu.Unlock()
		if line != "" {
			syncLog(line)
		}
		if pictureLine != "" {
			frameLog(pictureLine)
		}
	}()
	if len(e.queue) == 0 {
		// The playhead still moves with no picture to show: the log says
		// where it stands, and a seek asks what the sound is doing long
		// before the queue has anything in it.
		pos = e.posLocked(t)
		changed := e.current.Surface != e.returned.Surface
		e.returned = e.current
		return e.current, changed, e.current.Surface != nil
	}
	// How often the window asks for a picture: the lead above is meant to
	// cover that wait, and the log says what it really is.
	if !e.frameAt.IsZero() {
		if d := t.Sub(e.frameAt); d > 0 && d < 200*time.Millisecond {
			if e.frameStep == 0 {
				e.frameStep = d
			} else {
				e.frameStep = (e.frameStep*7 + d) / 8
			}
		}
	}
	e.frameAt = t
	pos = e.posLocked(t)
	// The picture goes up a frame or two after the engine hands it over, at
	// the window's next draw, so the one due now is the one for a little
	// further on: without it the picture trails the sound by that draw.
	// lead is how far ahead of the clock a picture is handed over: the
	// window draws at its own frames, and the picture waits on average half
	// of that interval before one comes, not the whole of it. Measured, not
	// guessed: this machine draws a 4K texture every 17ms, so a full
	// interval of 17ms reached past the delay it was meant to cover, which
	// the ear still heard as the sound a little late.
	// The next picture is due when the playhead reaches its own time: its
	// at is when it should stand on the screen, and the window that asks now
	// replaces the one showing once that time has come. mpv hands the vo a
	// future pts and the vo waits for it (video.c:1292, vo.c: vo_is_ready_
	// for_frame); this window does not wait on a future time, so the sound
	// the device still holds is not part of the moment at all — adding it
	// here showed a picture of pos+200ms at pos, a fifth of a second ahead.
	iff := pos + e.leadLocked()
	// Drop every picture the playhead passed, keeping the one showing.
	var gone []ui.VideoPicture
	for len(e.queue) > 1 && e.queue[1].at <= iff {
		gone = append(gone, e.queue[0].pic)
		e.queue = e.queue[1:]
	}
	if e.queue[0].at <= iff {
		next := e.queue[0].pic
		changed := next.Surface != e.returned.Surface
		if e.current.Surface != nil && e.current.Surface != next.Surface {
			gone = append(gone, e.current)
		}
		e.current = next
		e.last = e.queue[0].at
		e.shownVideo, e.shownAudio, e.shown = e.last, pos, true
		if len(e.queue) > 1 {
			e.queue = e.queue[1:]
		}
		// The pictures the playhead passed keep their textures until here:
		// releasing them lets the decoder convert the next ones into them.
		releasePictures(e.src, gone...)
		e.returned = e.current
		return e.current, changed, true
	}
	if e.current.Surface == nil {
		e.current = e.queue[0].pic
		e.last = e.queue[0].at
		e.shownVideo, e.shownAudio, e.shown = e.last, pos, true
		e.returned = e.current
		return e.current, true, true
	}
	// No picture is due: the one showing stands until its step is spent,
	// and leadLocked reads what is left of it off the clock.
	changed := e.current.Surface != e.returned.Surface
	e.returned = e.current
	return e.current, changed, true
}

// avOffset is how far the picture is moved against the sound, read from a
// small file so a machine whose output delay is not the engine's can be
// brought into line while it plays: a positive number shows the picture
// earlier, which is what a sound that runs ahead wants. Read once a second.
func (e *Engine) avOffset() time.Duration {
	now := time.Now()
	if e.avFixed {
		return e.avShift
	}
	if now.Sub(e.avReadAt) < time.Second {
		return e.avShift
	}
	e.avReadAt = now
	b, err := os.ReadFile(filepath.Join(os.TempDir(), "mygo-av-offset.txt"))
	if err != nil {
		return e.avShift
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return e.avShift
	}
	if n > 1000 {
		n = 1000
	}
	if n < -1000 {
		n = -1000
	}
	e.avShift = time.Duration(n) * time.Millisecond
	return e.avShift
}

// SetPresentLag takes what the last present took from its call to the screen
// taking it, measured off the swap chain's own statistics and told with each
// frame the window asks for. It is the part of a picture's way to the eye
// that no clock inside the engine sees, and the lead a picture is handed over
// by follows it: mpv reads its presentation feedback for the same
// (video/out/vo.c: check_timed_frame_late, the late.lead it keeps).
func (e *Engine) SetPresentLag(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d <= 0 {
		return // nothing measured: keep what a display's own period gave
	}
	if e.presentLag == 0 {
		e.presentLag = d
		return
	}
	// A gentle average: the measure jumps a refresh apart by what the
	// compositor was busy with, and the lead must not.
	e.presentLag = (e.presentLag*7 + d) / 8
}

// SetAVOffset moves the picture against the sound while a playback runs: a
// positive offset shows the picture earlier, which is what a sound that runs
// ahead of it wants. It is the audio delay a player offers, and it outlasts
// the file that holds one, which is only for finding the value.
func (e *Engine) SetAVOffset(d time.Duration) {
	e.mu.Lock()
	e.avShift = d
	e.avFixed = true
	e.mu.Unlock()
}

// AVOffset is how far the picture stands from the sound.
func (e *Engine) AVOffset() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.avShift
}

// tracing says whether a file asks for every picture to be written down, read
// once a second.
func (e *Engine) tracing() bool {
	now := time.Now()
	if now.Sub(e.traceAt) < time.Second {
		return e.traceOn
	}
	e.traceAt = now
	_, err := os.Stat(filepath.Join(os.TempDir(), "mygo-trace"))
	e.traceOn = err == nil
	return e.traceOn
}

// leadLocked is what a shown picture holds the screen for, less what the
// device will play of the sound while it does: the account the next picture
// is due against, which is mpv's delay at the moment a frame is shown
// (video.c: delay -= frame_time before the next is looked for). The window
// asks at its own pace, so the account is read off the frame showing, not
// kept by hand across calls: it cannot drift.
func (e *Engine) leadLocked() time.Duration {
	step := e.step
	if step <= 0 {
		step = 40 * time.Millisecond
	}
	// The lead is what a present takes to reach the screen, read off the
	// swap chain where it measures itself (presentLag, told with each frame
	// the window asks for); where nothing measures it, half of a
	// compositor's period and a little for the drawing stands in. mpv reads
	// its presentation feedback for the same and keeps the lead it finds
	// (vo.c: check_timed_frame_late, late.lead).
	lead := e.presentLag
	if lead <= 0 {
		lead = composePeriod()/2 + 2*time.Millisecond
	}
	if lead < step/8 {
		lead = step / 8
	}
	// A picture handed over further ahead than its own step is the next one
	// shown before this one had its turn: the queue runs a frame ahead and
	// the picture drifts, which reads as the film moving unevenly. mpv holds
	// its own lead under a frame for the same.
	if lead > step {
		lead = step
	}
	return lead + e.avOffset()
}

// syncLine reports the playhead against the picture the screen now shows,
// read after that picture was chosen: a line made before it compared this
// moment's playhead with the last one's picture, and showed a gap of its own.
func (e *Engine) syncLine(pos time.Duration) string {
	q0, qn := time.Duration(-1), time.Duration(-1)
	if len(e.queue) > 0 {
		q0, qn = e.queue[0].at, e.queue[len(e.queue)-1].at
	}
	return fmt.Sprintf("pos=%8.3fs picture=%8.3fs gap=%+6dms left=%v delay=%v ui=%v queue=%2d [%s..%s] skipped=%v rate=%v ended=%v",
		pos.Seconds(), e.last.Seconds(), (e.last - pos).Milliseconds(),
		e.leadLocked(), e.sndDelay, e.frameStep.Round(time.Millisecond), len(e.queue),
		q0.String(), qn.String(), e.skipping, e.rate, e.ended)
}

// presentLagLine says what the swap chain measured of the last present's way
// to the screen, for the log: the lead follows it, and what it reads is worth
// seeing beside the gap.
func (e *Engine) presentLagLine() string {
	return fmt.Sprintf("present lag %v (lead %v)", e.presentLag.Round(time.Millisecond), e.leadLocked())
}

// Sync reports where the picture and the sound stood when the picture now
// showing was put up, read together as mpv reads them when it compares the
// two. A gap within a picture's own step is the picture waiting for its
// moment, which is not a drift.
func (e *Engine) Sync() (video, audio time.Duration, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.shownVideo, e.shownAudio, e.shown
}

// posLocked is where the playhead stands, for a thread that holds the lock:
// the sound's position, which is the clock, even before it moves. The wall
// clock only stands in where no sound plays, as a video without a track.
func (e *Engine) posLocked(t time.Time) time.Duration {
	// What the sound has played of this place, which is added to every way
	// the playhead can move.
	pos := e.at + e.audioPos
	if e.paused {
		return pos
	}
	if e.snd == nil {
		if e.clock.IsZero() {
			return pos
		}
		return pos + time.Duration(float64(t.Sub(e.clock))*e.rate)
	}
	// Before the device moves, its position is 0: the picture waits there
	// for it, rather than running ahead on the wall clock.
	if e.audioEndedAt.IsZero() {
		// The device reports whole periods — 20ms on this machine — so its
		// position jumps where the sound runs evenly, and a picture chosen
		// off a jumping clock jumps with it. The report is a point the
		// playhead passes through, and the wall clock carries it from there
		// until the device reports again, as mpv reads the sound's delay
		// once and then sleeps the time it has left.
		//
		// Only as far as the reports come, though: a device that has stopped
		// reporting has stopped playing, and a playhead carried on the wall
		// clock past that runs ahead of the sound by the whole of the wait —
		// then drops back the moment a report comes, which is a start that
		// jumps.
		if !e.audioAt.IsZero() {
			d := t.Sub(e.audioAt)
			// No more than the reports come: a device that stopped reporting
			// stopped playing, and the playhead carried on the wall clock
			// past that runs ahead of the sound by the whole of the wait.
			// Before the first report names the step it takes, one period of
			// the device stands in — a playhead frozen until then would stop
			// the picture between two reports.
			limit := 2 * e.audioStep
			if limit <= 0 {
				limit = 40 * time.Millisecond
			}
			if d > limit {
				d = limit
			}
			if d > 0 {
				pos += time.Duration(float64(d) * e.rate)
			}
		}
		return pos
	}
	// The sound is over, the picture is not: it goes on by the wall clock,
	// from where the sound left it.
	return pos + time.Duration(float64(t.Sub(e.audioEndedAt))*e.rate)
}

// Interval is how long until the next picture comes due: the playhead's own
// distance to it, less the lead a present takes. The window schedules its
// next frame at exactly that (askNext: c.After(interval)), which is what
// mpv's vo does with a frame's time — vo_is_ready_for_frame holds until the
// pts, and a timer wakes it (wakeup_locked). Asked at a fixed step instead,
// the window's own pace quantised the moment every picture went up by ±31ms,
// and no choice of lead could take that out: the lead moved the average
// while the quantisation kept the swing.
func (e *Engine) Interval() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.queue) == 0 || e.ended {
		return 40 * time.Millisecond
	}
	next := e.queue[0].at - e.posLocked(time.Now()) - e.leadLocked()
	if next < time.Millisecond {
		return time.Millisecond
	}
	if next > 200*time.Millisecond {
		return 200 * time.Millisecond
	}
	return next
}

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
	return e.posLocked(time.Now())
}

// Play starts or resumes the video.
func (e *Engine) Play() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.clock.IsZero() || e.paused {
		e.clock = time.Now()
		// The device carried on from where the pause left it, and the
		// wall clock carries the playhead from here, not from the pause.
		e.audioAt = time.Now()
	}
	e.paused = false
}

// Pause stops the video where it stands.
func (e *Engine) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.paused {
		return
	}
	// Only while the wall clock carries the playhead is there anything to
	// fold in: the sound's position is a place already played, and adding
	// the time since the playback began to it counts that time twice, which
	// put the playhead past the end of the film and started the next one.
	if e.snd == nil {
		if !e.clock.IsZero() {
			e.at += time.Duration(float64(time.Since(e.clock)) * e.rate)
			e.clock = time.Time{}
		}
	} else if !e.audioEndedAt.IsZero() {
		e.audioPos += time.Duration(float64(time.Since(e.audioEndedAt)) * e.rate)
		e.audioEndedAt = time.Time{}
	}
	e.paused = true
}

// Seek goes to d, restarting the decoder and the sound there.
func (e *Engine) Seek(d time.Duration) {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return
	}
	oldQueue, oldCurrent := e.queue, e.current
	e.queue = nil
	e.at = d
	e.base = d
	e.last = d
	e.clock = time.Now()
	e.ended = false
	e.state = ui.VideoLoading
	e.audioPos = 0
	e.audioAt = time.Time{}
	e.audioEndedAt = time.Time{}
	e.current = ui.VideoPicture{}
	e.returned = ui.VideoPicture{}
	src, snd := e.src, e.snd
	if snd != nil {
		e.sndSeek = &d
		e.sndPending = true
		// The sound holds at the new place until a picture of it is ready,
		// as mpv holds its audio start (audio.c: audio_start_ao).
		e.starting = true
	}
	// A drag sends many seeks and each ran on a goroutine of its own, so
	// the decoder was sought in whatever order they took its lock: it could
	// end on an old place while the engine stood on the newest, every picture
	// decoded past was dropped for belonging to a place no longer asked for,
	// and the picture froze for good — a pause or a play moves nothing.
	// mpv merges seeks (the deferring in mpctx->seek); this one numbers them
	// and only the last runs.
	e.seekSeq++
	seq := e.seekSeq
	if src != nil {
		// The decoder is not freed until a seek that is already inside it
		// has returned: Close waits on this, rather than freeing under it.
		e.seekWG.Add(1)
	}
	e.mu.Unlock()
	if src != nil {
		// The pictures of the place left behind still hold their slots:
		// give them back before the decoder converts the pictures of the
		// new place.
		pics := make([]ui.VideoPicture, 0, len(oldQueue)+1)
		for _, q := range oldQueue {
			pics = append(pics, q.pic)
		}
		if oldCurrent.Surface != nil {
			pics = append(pics, oldCurrent)
		}
		releasePictures(src, pics...)
	}
	if src != nil {
		go func() {
			defer e.seekWG.Done()
			e.mu.Lock()
			latest := e.seekSeq
			e.mu.Unlock()
			if seq != latest {
				return
			}
			err := src.Seek(d)
			e.mu.Lock()
			e.seekLanded = seq
			if e.seekSeq == seq {
				if err != nil {
					// A seek that did not land leaves the pictures where
					// they were while the engine stands at the place
					// asked for: say so, rather than wait for a picture
					// that is not coming.
					e.err = err
					e.state = ui.VideoFailed
				} else {
					e.state = ui.VideoReady
				}
			}
			e.mu.Unlock()
		}()
	}
}

// SetLoop is not wired yet.
func (e *Engine) SetLoop(v bool) {}

// SetRate changes how fast the video plays.
func (e *Engine) SetRate(r float64) {
	if r <= 0 {
		r = 1
	}
	e.mu.Lock()
	if e.rate == r {
		e.mu.Unlock()
		return
	}
	// The place is read with the old rate, then anchored again under the
	// new one: the sound the device holds was made at the old speed, and
	// scaling the whole of its clock by the new one would move the playhead
	// by however long the film had run. mpv rebuilds its filter chain for
	// the same reason; a seek is this engine's way of starting the count
	// again at the place showing.
	pos := e.posLocked(time.Now())
	hasSrc := e.src != nil
	e.rate = r
	e.mu.Unlock()
	if hasSrc {
		e.Seek(pos)
	}
}

// SetVolume sets how loud the sound plays, from 0 to 1.
func (e *Engine) SetVolume(v float64) {
	e.mu.Lock()
	e.volume = min(max(v, 0), 1)
	e.mu.Unlock()
}

// SetMuted silences the sound, or plays it again.
func (e *Engine) SetMuted(v bool) {
	e.mu.Lock()
	e.muted = v
	e.mu.Unlock()
}

// Close ends the decoder and the sound. It waits for the threads that play
// them: a device freed while its thread still fills it is a crash.
func (e *Engine) Close() error {
	e.mu.Lock()
	e.stopped = true
	src, snd := e.src, e.snd
	queue, current := e.queue, e.current
	abort, finish := e.abortIO, e.finishIO
	e.abortIO, e.finishIO = nil, nil
	e.mu.Unlock()
	// A read waiting on the network gives up at once: the threads below
	// are waited on, and one waiting out a timeout would hold the close.
	if abort != nil {
		abort()
	}
	e.wg.Wait()
	e.seekWG.Wait()
	if src != nil {
		pics := make([]ui.VideoPicture, 0, len(queue)+1)
		for _, q := range queue {
			pics = append(pics, q.pic)
		}
		if current.Surface != nil {
			pics = append(pics, current)
		}
		releasePictures(src, pics...)
	}
	if snd != nil {
		snd.Close()
	}
	if src != nil {
		err := src.Close()
		if finish != nil {
			finish()
		}
		return err
	}
	if finish != nil {
		finish()
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
		// What the engine plays by, not a read of the device: the device
		// still reports the place before a seek until the seek reaches it,
		// and this is called while holding the lock.
		s.Audio = e.at + e.audioPos
	}
	if !e.clock.IsZero() {
		s.Real = now.Sub(e.clock)
	}
	if e.current.Surface != nil {
		// Both on the file's own timeline, as the sound is now: a check that
		// lines them up compares two places of the film, not one place and
		// one stretch from a seek.
		s.Video = e.last
	}
	return s
}

// audioLog writes a line for the log of the sound and the playback, where a
// launch that plays none leaves what it got to.
func audioLog(msg string) {
	appendLog(filepath.Join(os.TempDir(), "mygo-audio.log"), msg)
}

// logMax is how large one of the logs may grow before it is started over:
// a playback left running writes its frame and sync lines all day.
const logMax = 4 << 20

// appendLog adds a line to a log, and starts it over once it is too large
// to read: the last playback is what the next one looks at.
func appendLog(p, msg string) {
	if fi, err := os.Stat(p); err == nil && fi.Size() > logMax {
		_ = os.Remove(p)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05.000"), msg)
}

// syncLog writes the playhead and the picture's place side by side, so the
// gap between the sound and the picture can be read after a playback.
func syncLog(msg string) {
	appendLog(filepath.Join(os.TempDir(), "mygo-sync.log"), msg)
}

// frameLog writes every picture the screen is given, with the wall clock it
// was given at: against a recording of the window, that is the time a picture
// takes to reach the screen, which no clock inside the player can know. It
// only writes while a file asks for it, as a playback of twenty seconds is
// five hundred lines.
func frameLog(msg string) {
	appendLog(filepath.Join(os.TempDir(), "mygo-frames.log"), msg)
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
