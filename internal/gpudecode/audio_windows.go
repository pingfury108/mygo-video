//go:build windows

package gpudecode

/*
#include "audio_windows.c"
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"
)

// Audio plays the sound of a video with WASAPI.
type Audio struct {
	a      C.Audio
	rate   int
	closed bool
}

// openAudio reads the sound of url and asks the output device for its format.
func openAudio(url string, headers []string, start time.Duration) (*Audio, error) {
	cu := C.CString(url)
	defer C.free(unsafe.Pointer(cu))
	var ch *C.char
	if h := headerLines(headers); h != "" {
		ch = C.CString(h)
		defer C.free(unsafe.Pointer(ch))
	}
	p := &Audio{}
	var rate, channels C.int
	if rc := C.audio_open(&p.a, cu, ch, C.int64_t(start.Microseconds()), &rate, &channels); rc != 0 {
		return nil, fmt.Errorf("gpudecode: audio at step %d", int(rc))
	}
	p.rate = int(rate)
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
