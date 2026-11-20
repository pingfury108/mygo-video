package gpudecode

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// fakeFrames hands out pictures at a step, after a delay each.
type fakeFrames struct {
	mu    sync.Mutex
	step  time.Duration
	slow  time.Duration
	n     int
	ended bool
}

func (f *fakeFrames) Next() (ui.VideoPicture, error) {
	if f.slow > 0 {
		time.Sleep(f.slow)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ended {
		return ui.VideoPicture{}, io.EOF
	}
	f.n++
	pix := make([]byte, 4)
	return ui.NewPixelsPicture(pix, 1, 1), nil
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
func (c *fakeClock) Play() {
	c.mu.Lock()
	if c.start.IsZero() {
		c.start = time.Now()
	}
	c.mu.Unlock()
}
func (c *fakeClock) Pause() {
	c.mu.Lock()
	c.base = c.Position()
	c.start = time.Time{}
	c.mu.Unlock()
}
func (c *fakeClock) Close() {}

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
	if s.Video > s.Audio {
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
