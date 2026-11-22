//go:build windows

package gpudecode

/*
#include "audio_windows.c"
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"
)

// errNoAudio says the file has no audio stream, which is not a failure of
// the picture: it plays without sound.
var errNoAudio = errors.New("gpudecode: no audio stream")

// errAudioSeek says the sound could not be moved to the place the picture
// begins at, which would leave the two apart for the rest of the film.
var errAudioSeek = errors.New("gpudecode: sound seek at open")

// lockSoundThread pins the sound's thread and gives it a COM apartment of its
// own. A goroutine moves between system threads, and CoInitializeEx belongs to
// the thread that called it: a sound device made on one thread and filled from
// another reached WASAPI from a thread with no apartment, and hung there.
func lockSoundThread() {
	runtime.LockOSThread()
	C.audio_co_init()
}

// Audio plays the sound of a video with WASAPI.
type Audio struct {
	a      C.Audio
	rate   int
	closed bool
}

// openAudio reads the sound of url and asks the output device for its format.
func openAudio(url string, headers []string, start time.Duration, rate float64, abort unsafe.Pointer) (*Audio, error) {
	cu := C.CString(url)
	defer C.free(unsafe.Pointer(cu))
	var ch *C.char
	if h := headerLines(headers); h != "" {
		ch = C.CString(h)
		defer C.free(unsafe.Pointer(ch))
	}
	p := &Audio{}
	var deviceRate, channels C.int
	if rc := C.audio_open(&p.a, cu, ch, C.int64_t(start.Microseconds()), C.double(rate), abort, &deviceRate, &channels); rc != 0 {
		// audio_open may have made the input, the decoder, or the device
		// before the step that failed: close gives them all back.
		C.audio_close(&p.a)
		if rc == -3 {
			return nil, errNoAudio
		}
		if rc == -19 {
			return nil, errAudioSeek
		}
		return nil, fmt.Errorf("gpudecode: audio at step %d", int(rc))
	}
	p.rate = int(deviceRate)
	return p, nil
}

// Fill writes what the device plays next, and returns how many frames it wrote.
func (p *Audio) Fill() int { return int(C.audio_fill(&p.a)) }

// Play starts the device.
func (p *Audio) Play() { C.audio_play(&p.a) }

// Pause stops the device where it stands.
func (p *Audio) Pause() { C.audio_pause(&p.a) }

// Position is how far the device has played.
func (p *Audio) Position() time.Duration {
	return time.Duration(int64(C.audio_position(&p.a))) * time.Microsecond
}

// Delay is how long the sound it holds will take to play.
func (p *Audio) Delay() time.Duration {
	return time.Duration(int64(C.audio_delay_us(&p.a))) * time.Microsecond
}

// Close releases the device.
func (p *Audio) Close() {
	if p.closed {
		return
	}
	p.closed = true
	C.audio_close(&p.a)
}

// Ended says the sound of the file is over: a picture that outlasts it goes
// on by the wall clock rather than freezing at the last of the sound.
func (p *Audio) Ended() bool { return C.audio_ended(&p.a) != 0 }

// Seek goes to d in the sound, which the device plays from there.
func (p *Audio) Seek(d time.Duration) int {
	return int(C.audio_seek(&p.a, C.int64_t(d.Microseconds())))
}

// SetVolume sets the session's level, from 0 to 1.
func (p *Audio) SetVolume(v float64) int {
	return int(C.audio_set_volume(&p.a, C.float(v)))
}

// SetMuted silences the session, or plays it again.
func (p *Audio) SetMuted(m bool) int {
	n := 0
	if m {
		n = 1
	}
	return int(C.audio_set_mute(&p.a, C.int(n)))
}

// SetRate makes the film play r times as fast.
func (p *Audio) SetRate(r float64) int {
	return int(C.audio_set_speed(&p.a, C.double(r)))
}
