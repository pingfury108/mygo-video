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

	mu      sync.Mutex
	wg      sync.WaitGroup // the decoder and the sound, until they stop
	wake    func()
	queue   []queued
	current ui.VideoPicture
	base    time.Duration

	at       time.Duration // where the playhead stood when playback began
	audioPos time.Duration // where the sound stood, read off the frame
	// audioAt is when the device last reported, which the playhead is
	// carried from by the wall clock: the report is whole periods apart.
	audioAt time.Time
	clock   time.Time // when playback began, zero while paused
	paused  bool
	rate    float64
	// sndSeek is a seek for the thread that owns the device to make: the
	// call reads the source and must not hold the window.
	sndSeek *time.Duration
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
	// The sound's calls reach WASAPI, whose apartment belongs to a thread:
	// the thread is pinned and given one of its own here.
	lockSoundThread()
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
	for {
		e.mu.Lock()
		stopped, paused := e.stopped, e.paused
		seekReq := e.sndSeek
		e.sndSeek = nil
		e.mu.Unlock()
		if stopped {
			return
		}
		// The sound is sought here, on its own thread: the seek reads the
		// source over the network, and a window waiting for that would stop
		// drawing for as long as it took.
		if seekReq != nil {
			e.snd.Seek(*seekReq)
			continue
		}
		if paused {
			e.snd.Pause()
			time.Sleep(30 * time.Millisecond)
			continue
		}
		e.snd.Play()
		n := e.snd.Fill()
		if n <= 0 {
			// Nothing was written: the sound is over, or the source stalled.
			// A device that drives the writing gave its own wait already.
			time.Sleep(5 * time.Millisecond)
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
			if !e.paused {
				// The device reports whole periods, a little behind the
				// sound each time: taking it as it comes would step the
				// playhead back by up to a period on every report, and the
				// picture would step with it. The playhead only moves on
				// from where it is, and takes the report when the two have
				// drifted apart for good.
				now := time.Now()
				predicted := e.audioPos
				if !e.audioAt.IsZero() {
					predicted += time.Duration(float64(now.Sub(e.audioAt)) * e.rate)
				}
				if p > predicted || predicted-p > 60*time.Millisecond {
					e.audioPos = p
				}
				e.audioAt = now
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
		if e.stopped {
			e.mu.Unlock()
			return
		}
		src, paused, queueLen := e.src, e.paused, len(e.queue)
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
		// A seek lands before the place asked for, and the pictures of that
		// place belong to where the playhead no longer is: they are decoded
		// past, not shown, so the picture starts where the seek asked for, as
		// the sound does.
		if off < 0 {
			e.mu.Unlock()
			continue
		}
		// The playhead ran past everything ready by a wide margin: a jump
		// stands on the newest picture now, rather than flashing through
		// every one of the ones it passed.
		lag := e.posLocked(time.Now()) - (e.base + off)
		if lag > lagJump {
			e.queue = nil
			e.current = pic
			e.last = e.base + off
			e.mu.Unlock()
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
		wk := e.wake
		e.mu.Unlock()
		if wk != nil {
			wk()
		}
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
			line = e.syncLine(pos)
		}
		// Every call the window makes, not just the calls that bring a new
		// picture: the times between them are the window's own rhythm, which
		// decides when a picture can reach the screen at all.
		if e.tracing() {
			fresh := "same"
			if e.last != e.traceLast {
				e.traceLast = e.last
				fresh = "new"
			}
			pictureLine = fmt.Sprintf("%s picture %.3f pos %.3f left %v delay %v", fresh, e.last.Seconds(), pos.Seconds(), e.leadLocked().Milliseconds(), e.sndDelay.Milliseconds())
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
		return e.current, false, e.current.Surface != nil
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
	for len(e.queue) > 1 && e.queue[1].at <= iff {
		e.queue = e.queue[1:]
	}
	if e.queue[0].at <= iff {
		changed := e.current.Surface != e.queue[0].pic.Surface
		e.current = e.queue[0].pic
		e.last = e.queue[0].at
		e.shownVideo, e.shownAudio, e.shown = e.last, pos, true
		if len(e.queue) > 1 {
			e.queue = e.queue[1:]
		}
		return e.current, changed, true
	}
	if e.current.Surface == nil {
		e.current = e.queue[0].pic
		e.last = e.queue[0].at
		e.shownVideo, e.shownAudio, e.shown = e.last, pos, true
		return e.current, true, true
	}
	// No picture is due: the one showing stands until its step is spent,
	// and leadLocked reads what is left of it off the clock.
	return e.current, false, true
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
	// The lead is what a present takes: a frame handed over now reaches the
	// screen at the compositor's next draw, which is on average half its
	// period away, and a little past that for the drawing. Measured where it
	// can be (composePeriod), a display's own where it cannot. mpv presents
	// its vo's frames half a vsync ahead of their vsync for the same reason
	// (vo.c: target = due - vsync_interval*(1+lead)/2).
	lead := composePeriod()/2 + 2*time.Millisecond
	if lead < step/8 {
		lead = step / 8
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
		if !e.audioAt.IsZero() {
			pos += time.Duration(float64(t.Sub(e.audioAt)) * e.rate)
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
	src, snd := e.src, e.snd
	if snd != nil {
		e.sndSeek = &d
	}
	e.mu.Unlock()
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

// syncLog writes the playhead and the picture's place side by side, so the
// gap between the sound and the picture can be read after a playback.
func syncLog(msg string) {
	p := filepath.Join(os.TempDir(), "mygo-sync.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05.000"), msg)
}

// frameLog writes every picture the screen is given, with the wall clock it
// was given at: against a recording of the window, that is the time a picture
// takes to reach the screen, which no clock inside the player can know. It
// only writes while a file asks for it, as a playback of twenty seconds is
// five hundred lines.
func frameLog(msg string) {
	p := filepath.Join(os.TempDir(), "mygo-frames.log")
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
