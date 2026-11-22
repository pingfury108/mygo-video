package gpudecode

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// fakeFrames hands out pictures at a step, after a delay each. Offsets may
// follow a table of their own, for a video whose frames are not evenly
// spaced. A decoder asked to skip passes them without its decoding delay,
// as skipping reference-frame work really does.
type fakeFrames struct {
	mu       sync.Mutex
	step     time.Duration
	slow     time.Duration
	offsets  []time.Duration
	n        int
	ended    bool
	skipping bool
	skips    int
	released int
}

func (f *fakeFrames) Next() (ui.VideoPicture, time.Duration, error) {
	if f.slow > 0 && !f.skipping {
		time.Sleep(f.slow)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ended {
		return ui.VideoPicture{}, 0, io.EOF
	}
	off := time.Duration(f.n) * f.step
	if f.offsets != nil {
		if f.n < len(f.offsets) {
			off = f.offsets[f.n]
		} else {
			off = f.offsets[len(f.offsets)-1] + time.Duration(f.n-len(f.offsets)+1)*f.step
		}
	}
	f.n++
	pix := make([]byte, 4)
	return ui.NewPixelsPicture(pix, 1, 1), off, nil
}

func (f *fakeFrames) SetSkip(on bool) {
	f.mu.Lock()
	if on != f.skipping {
		f.skips++
	}
	f.skipping = on
	f.mu.Unlock()
}

func (f *fakeFrames) Release(ui.VideoPicture) {
	f.mu.Lock()
	f.released++
	f.mu.Unlock()
}

func (f *fakeFrames) skipCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.skips
}

func (f *fakeFrames) releaseCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.released
}

func (f *fakeFrames) Seek(d time.Duration) error { return nil }
func (f *fakeFrames) Step() time.Duration        { return f.step }
func (f *fakeFrames) Duration() time.Duration    { return time.Hour }
func (f *fakeFrames) Size() (int, int)           { return 1, 1 }
func (f *fakeFrames) Close() error               { return nil }

// fakeClock plays at a rate of its own, as a device that runs fast or slow.
type fakeClock struct {
	mu    sync.Mutex
	rate  float64
	start time.Time
	base  time.Duration
	ended bool
}

func (c *fakeClock) Position() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.start.IsZero() {
		return c.base
	}
	return c.base + time.Duration(float64(time.Since(c.start))*c.rate)
}

func (c *fakeClock) Seek(d time.Duration) int {
	c.mu.Lock()
	c.base = d
	c.start = time.Now()
	c.mu.Unlock()
	return 0
}

func (c *fakeClock) Fill() int { return 1 }

// Delay is the buffer a real device would hold: two steps of it.
func (c *fakeClock) Delay() time.Duration { return 80 * time.Millisecond }
func (c *fakeClock) Play() {
	c.mu.Lock()
	if c.start.IsZero() {
		c.start = time.Now()
	}
	c.mu.Unlock()
}
func (c *fakeClock) Pause() {
	c.mu.Lock()
	// Position takes this lock too: the elapsed time is folded in directly,
	// a call from here would lock against itself.
	if !c.start.IsZero() {
		c.base += time.Duration(float64(time.Since(c.start)) * c.rate)
	}
	c.start = time.Time{}
	c.mu.Unlock()
}
func (c *fakeClock) Close() {}

// done is where a test makes the sound run out, as a film whose closing
// titles outlast its last sound does.
func (c *fakeClock) Done() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ended
}

// TestPictureFollowsSound is the check of the sync: with the sound at 1x, the
// picture shown stands where the sound does.
func TestPictureFollowsSound(t *testing.T) {
	src := &fakeFrames{step: 40 * time.Millisecond}
	snd := &fakeClock{rate: 1}
	e := newEngine(src, snd)
	defer e.Close()
	e.Play()

	deadline := time.Now().Add(1200 * time.Millisecond)
	for time.Now().Before(deadline) {
		e.Frame(time.Now())
		time.Sleep(10 * time.Millisecond)
	}
	s := e.Stat(time.Now())
	drift := s.Video - s.Audio
	t.Logf("video=%v sound=%v drift=%v queue=%d", s.Video, s.Audio, drift, s.Queue)
	if drift < -200*time.Millisecond || drift > 200*time.Millisecond {
		t.Fatalf("picture at %v, sound at %v: %v apart", s.Video, s.Audio, drift)
	}
}

// TestPictureKeepsUpWithFastSound is what a device that runs fast does: the
// picture drops the frames it passed, and stands where the sound does rather
// than falling behind.
func TestPictureKeepsUpWithFastSound(t *testing.T) {
	src := &fakeFrames{step: 20 * time.Millisecond}
	snd := &fakeClock{rate: 1.2}
	e := newEngine(src, snd)
	defer e.Close()
	e.Play()

	deadline := time.Now().Add(1200 * time.Millisecond)
	for time.Now().Before(deadline) {
		e.Frame(time.Now())
		time.Sleep(10 * time.Millisecond)
	}
	s := e.Stat(time.Now())
	drift := s.Video - s.Audio
	t.Logf("video=%v sound=%v drift=%v", s.Video, s.Audio, drift)
	if drift < -300*time.Millisecond {
		t.Fatalf("picture fell behind a fast sound by %v", drift)
	}
}

// TestSlowDecoderStallsPicture is the stall seen in the app: a decoder slower
// than the video leaves the queue empty, and the picture stops at the last one.
func TestSlowDecoderStallsPicture(t *testing.T) {
	src := &fakeFrames{step: 40 * time.Millisecond, slow: 80 * time.Millisecond}
	snd := &fakeClock{rate: 1}
	e := newEngine(src, snd)
	defer e.Close()
	e.Play()

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		e.Frame(time.Now())
		time.Sleep(10 * time.Millisecond)
	}
	s := e.Stat(time.Now())
	t.Logf("slow decoder: video=%v sound=%v queue=%d", s.Video, s.Audio, s.Queue)
	// The picture is handed over a refresh before its time, and the
	// playhead is carried by the wall clock from the device's last report:
	// both let it stand a little ahead of the sound. This looks for a stall,
	// which leaves it a whole step or more ahead.
	if s.Video > s.Audio+100*time.Millisecond {
		t.Fatalf("picture ahead of the sound with a slow decoder")
	}
}

// TestSeekMovesBoth is a seek of the picture and the sound to the same place.
func TestSeekMovesBoth(t *testing.T) {
	src := &fakeFrames{step: 40 * time.Millisecond}
	snd := &fakeClock{rate: 1}
	e := newEngine(src, snd)
	defer e.Close()
	e.Play()

	time.Sleep(200 * time.Millisecond)
	e.Seek(30 * time.Minute)
	time.Sleep(300 * time.Millisecond)
	s := e.Stat(time.Now())
	t.Logf("after seek: video=%v sound=%v", s.Video, s.Audio)
	if s.Audio < 30*time.Minute {
		t.Fatalf("sound at %v after a seek to 30m", s.Audio)
	}
}

// TestEngineBeforeItsSources is the engine as the app makes it: the decoder and
// the sound open apart, so an engine starts with neither, and must not crash.
func TestEngineBeforeItsSources(t *testing.T) {
	e := newEngine(nil, nil)
	if e == nil {
		t.Fatal("no engine")
	}
	if _, _, ok := e.Frame(time.Now()); ok {
		t.Fatal("a picture before any was decoded")
	}
	if st, _ := e.State(); st != ui.VideoLoading {
		t.Fatalf("state %v before the sources opened", st)
	}
	e.Close()
}

// TestCloseWithSources is the close the app does: every thread that plays must
// be counted, so waiting for them does not see a counter that went under.
func TestCloseWithSources(t *testing.T) {
	src := &fakeFrames{step: 40 * time.Millisecond}
	snd := &fakeClock{rate: 1}
	e := newEngine(src, snd)
	e.Play()
	time.Sleep(200 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		e.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close waited for threads that never ended")
	}
}

// TestVFRShowsOnItsOwnTimeline is what a variable frame rate caught wrong
// before: the frames were timed by counting them at the average rate, and a
// video whose frames are uneven drifted away from the sound. On its own
// offsets each picture shows when its offset comes due, however uneven
// they are.
func TestVFRShowsOnItsOwnTimeline(t *testing.T) {
	offsets := []time.Duration{0, 40 * time.Millisecond, 240 * time.Millisecond, 280 * time.Millisecond, 560 * time.Millisecond}
	src := &fakeFrames{
		step:    20 * time.Millisecond,
		offsets: offsets,
	}
	snd := &fakeClock{rate: 1}
	e := newEngine(src, snd)
	defer e.Close()
	e.Play()

	// When each picture first shows, in the sound's time: a picture whose
	// offset says 240ms has to turn up around there, not a count of frames
	// later.
	shown := map[time.Duration]time.Duration{}
	last := time.Duration(-1)
	deadline := time.Now().Add(900 * time.Millisecond)
	for time.Now().Before(deadline) {
		now := time.Now()
		e.Frame(now)
		s := e.Stat(now)
		if s.Video != last {
			if _, ok := shown[s.Video]; !ok && last >= 0 {
				shown[s.Video] = s.Audio
			}
			last = s.Video
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, off := range offsets[1:] {
		at, ok := shown[off]
		if !ok {
			t.Fatalf("the picture at %v never showed", off)
		}
		if d := at - off; d < -100*time.Millisecond || d > 200*time.Millisecond {
			t.Fatalf("the picture at %v turned up %v into the sound", off, d)
		}
	}
}

// TestQueueKeepsAboutASecond is the depth that keeps the picture playing
// while the sound does on a slow source: as deep as the sound's buffer.
func TestQueueKeepsAboutASecond(t *testing.T) {
	src := &fakeFrames{step: 40 * time.Millisecond} // 25fps: a second is 25
	snd := &fakeClock{rate: 1}
	e := newEngine(src, snd)
	defer e.Close()
	e.Play()

	deadline := time.Now().Add(800 * time.Millisecond)
	max := 0
	for time.Now().Before(deadline) {
		e.Frame(time.Now())
		time.Sleep(10 * time.Millisecond)
		if q := e.Stat(time.Now()).Queue; q > max {
			max = q
		}
	}
	if max < 20 {
		t.Fatalf("queue held %d pictures, want about a second of them", max)
	}
	t.Logf("queue peaked at %d pictures (25 is a second)", max)
}

// TestLagTurnsSkipOn is mpv's set_frame_drops: a decoder too slow for the
// video lets the playhead fall behind, and past half a second the engine
// asks the decoder to pass over the frames that are not reference ones,
// and back off once it has caught up.
func TestLagTurnsSkipOn(t *testing.T) {
	// Decoding at half the video's pace: the playhead keeps falling behind.
	src := &fakeFrames{step: 40 * time.Millisecond, slow: 90 * time.Millisecond}
	snd := &fakeClock{rate: 1}
	e := newEngine(src, snd)
	defer e.Close()
	e.Play()

	time.Sleep(2 * time.Second)
	if src.skipCount() == 0 {
		t.Fatal("the decoder was never asked to skip")
	}
	t.Logf("skip turned on %d time(s) in %v of playback", src.skipCount(), 2*time.Second)
}

// TestHugeLagJumpsToNewest is the ffplay threshold: when the playhead is
// past every ready picture by seconds, showing them one by one is a
// slideshow — the newest picture stands in at once.
func TestHugeLagJumpsToNewest(t *testing.T) {
	src := &fakeFrames{step: 40 * time.Millisecond}
	snd := &fakeClock{rate: 1}
	e := newEngine(src, snd)
	defer e.Close()
	e.Play()

	// A pause keeps the queue from emptying, and the clock jump afterwards
	// puts the playhead seconds past what it holds.
	time.Sleep(1500 * time.Millisecond)
	e.Pause()
	time.Sleep(100 * time.Millisecond)
	snd.Seek(2800 * time.Millisecond)
	e.Play()

	deadline := time.Now().Add(500 * time.Millisecond)
	lastVideo := time.Duration(-1)
	for time.Now().Before(deadline) {
		e.Frame(time.Now())
		s := e.Stat(time.Now())
		if s.Video != lastVideo {
			lastVideo = s.Video
		}
		time.Sleep(10 * time.Millisecond)
	}
	s := e.Stat(time.Now())
	if s.Video < 2300*time.Millisecond {
		t.Fatalf("picture at %v after the playhead jumped to 2.8s", s.Video)
	}
	t.Logf("after a jump to 2.8s the picture stands at %v", s.Video)
}

// TestPictureOutlastsTheSound is the film whose closing titles run past its
// last sound: the sound stops, and the picture must go on to its own end
// rather than freezing at the last of it.
func TestPictureOutlastsTheSound(t *testing.T) {
	src := &fakeFrames{step: 40 * time.Millisecond}
	snd := &fakeClock{rate: 1}
	e := newEngine(src, snd)
	defer e.Close()
	e.Play()

	time.Sleep(400 * time.Millisecond)
	// The sound runs out where it stands, and the picture has seconds of
	// its own left.
	snd.mu.Lock()
	snd.ended = true
	snd.mu.Unlock()
	at := e.Stat(time.Now())
	time.Sleep(600 * time.Millisecond)
	for i := 0; i < 5; i++ {
		e.Frame(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	s := e.Stat(time.Now())
	if got := s.Video - at.Video; got < 300*time.Millisecond {
		t.Fatalf("picture moved %v after the sound ran out at %v", got, at.Video)
	}
	t.Logf("sound ended at %v, picture went on to %v", at.Video, s.Video)
}

// TestShownGapStaysWithinAStep is the measurement mpv uses: where the picture
// and the sound stood when the picture went up, read together. A gap inside
// one picture's step is the picture waiting for its moment, not a drift.
func TestShownGapStaysWithinAStep(t *testing.T) {
	step := 40 * time.Millisecond
	src := &fakeFrames{step: step}
	snd := &fakeClock{rate: 1}
	e := newEngine(src, snd)
	defer e.Close()
	e.Play()

	worst := time.Duration(0)
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		e.Frame(time.Now())
		if v, a, ok := e.Sync(); ok {
			if g := v - a; g < worst {
				worst = g
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if worst < -step {
		t.Fatalf("the shown picture stood %v behind the sound, past its own %v step", worst, step)
	}
	t.Logf("worst shown gap %v, one step is %v", worst, step)
}

// TestPositionAnswersWhilePlaying is the call the app makes every frame to draw
// the progress bar: it must answer, and answer while the window draws frames,
// which a playhead read under a lock it also takes would never do.
func TestPositionAnswersWhilePlaying(t *testing.T) {
	src := &fakeFrames{step: 40 * time.Millisecond}
	snd := &fakeClock{rate: 1}
	e := newEngine(src, snd)
	defer e.Close()
	e.Play()
	time.Sleep(100 * time.Millisecond)

	done := make(chan time.Duration, 1)
	go func() {
		e.Frame(time.Now())
		done <- e.Position()
	}()
	select {
	case pos := <-done:
		if pos <= 0 {
			t.Fatalf("position %v after 100ms of playback", pos)
		}
		t.Logf("position %v", pos)
	case <-time.After(2 * time.Second):
		t.Fatal("Position never answered: a lock taken twice")
	}
}

// TestReleaseGivesEachTextureBackOnce is the queue and the picture showing
// holding the same picture: it takes one slot, and must give back one.
func TestReleaseGivesEachTextureBackOnce(t *testing.T) {
	src := &fakeFrames{}
	p := ui.NewTexturePicture(41, 41, 1, 1)
	releasePictures(src, p, p)
	if n := src.releaseCount(); n != 1 {
		t.Fatalf("released %d times for one texture", n)
	}
	releasePictures(src, ui.NewTexturePicture(42, 42, 1, 1))
	if n := src.releaseCount(); n != 2 {
		t.Fatalf("released %d times for two textures", n)
	}
}

// TestPauseKeepsThePlaceItStopped is the pause after a playback: the place must
// stay where the picture stopped, not count the time already played a second
// time, which put the playhead past the end of the film and began the next one
// while the sound of this one still played.
func TestPauseKeepsThePlaceItStopped(t *testing.T) {
	src := &fakeFrames{step: 40 * time.Millisecond}
	snd := &fakeClock{rate: 1}
	e := newEngine(src, snd)
	defer e.Close()
	e.Play()
	time.Sleep(300 * time.Millisecond)

	before := e.Position()
	e.Pause()
	paused := e.Position()
	if paused > before+100*time.Millisecond {
		t.Fatalf("paused at %v, after playing %v: the time was counted twice", paused, before)
	}
	time.Sleep(200 * time.Millisecond)
	if after := e.Position(); after != paused {
		t.Fatalf("playhead moved from %v to %v while paused", paused, after)
	}
	t.Logf("paused at %v after %v of playback", paused, before)

	e.Play()
	time.Sleep(200 * time.Millisecond)
	if resumed := e.Position(); resumed <= paused {
		t.Fatalf("playhead stood at %v after resuming", resumed)
	}
}

// endingFrames is a decoder that reaches its end, and starts again where a
// seek puts it: the film the player plays once more from the bar.
type endingFrames struct {
	mu    sync.Mutex
	step  time.Duration
	n     int
	limit int
}

func (f *endingFrames) Next() (ui.VideoPicture, time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.n >= f.limit {
		return ui.VideoPicture{}, 0, io.EOF
	}
	off := time.Duration(f.n) * f.step
	f.n++
	return ui.NewPixelsPicture(make([]byte, 4), 1, 1), off, nil
}

func (f *endingFrames) Seek(d time.Duration) error {
	f.mu.Lock()
	f.n = int(d / f.step)
	f.mu.Unlock()
	return nil
}

func (f *endingFrames) Step() time.Duration     { return f.step }
func (f *endingFrames) Duration() time.Duration { return time.Duration(f.limit) * f.step }
func (f *endingFrames) Size() (int, int)        { return 1, 1 }
func (f *endingFrames) SetSkip(bool)            {}
func (f *endingFrames) Release(ui.VideoPicture) {}
func (f *endingFrames) Close() error            { return nil }

// longer makes the film run past where it ended, as a seek into the part of
// it the first play did not reach.
func (f *endingFrames) longer(n int) {
	f.mu.Lock()
	f.limit = n
	f.mu.Unlock()
}

// TestSeekAfterTheEndPlaysAgain is the bar taken back once the film ended:
// the decoder still stands at its end, and must be moved off it rather than
// left there with no thread to decode what follows.
func TestSeekAfterTheEndPlaysAgain(t *testing.T) {
	src := &endingFrames{step: 20 * time.Millisecond, limit: 5}
	e := newEngine(src, &fakeClock{rate: 1})
	defer e.Close()
	e.Play()
	waitState(t, e, ui.VideoEnded)

	// The part the first play did not reach: the pictures after the seek
	// must be ready before the film ends again.
	src.longer(5000)
	e.Seek(0)
	e.Play()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, _, ok := e.Frame(time.Now()); ok {
			if st, _ := e.State(); st == ui.VideoReady {
				break
			}
		}
		if time.Now().After(deadline) {
			st, err := e.State()
			t.Fatalf("state %v err %v after a seek from the end", st, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSeekWhilePausedShowsThePlace is the bar let go while the player is
// paused: the picture must go to the new place, as mpv shows it, rather than
// stand where it was until the play goes on.
func TestSeekWhilePausedShowsThePlace(t *testing.T) {
	src := &fakeFrames{step: 20 * time.Millisecond}
	e := newEngine(src, &fakeClock{rate: 1})
	defer e.Close()
	e.Play()
	time.Sleep(150 * time.Millisecond)
	e.Pause()

	e.Seek(30 * time.Second)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if pic, changed, ok := e.Frame(time.Now()); ok && changed && pic.Surface != nil {
			if pos := e.Position(); pos >= 30*time.Second {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("picture stayed at %v after a seek to 30s while paused", e.Position())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitState(t *testing.T, e *Engine, want ui.VideoState) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if st, _ := e.State(); st == want {
			return
		}
		if time.Now().After(deadline) {
			st, err := e.State()
			t.Fatalf("state %v err %v, wanted %v", st, err, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
