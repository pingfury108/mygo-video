//go:build windows

// Package gpudecode decodes a video with libavcodec and its Direct3D 11
// backend, keeping the pictures on the GPU: the system's media framework has
// no HEVC decoder on some machines, and a decoder that copies each frame
// through memory cannot carry a 4K picture fast enough.
package gpudecode

/*
#cgo CFLAGS: -I${SRCDIR}/ffmpeg/include
#cgo LDFLAGS: -L${SRCDIR}/ffmpeg/bin -lavcodec-63 -lavformat-63 -lavutil-61 -lswresample-7 -ldxguid -lole32 -luuid

#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_d3d11va.h>
#include <libavutil/pixfmt.h>
#include "decoder_windows.c"
#include "audio_windows.c"
*/
import "C"

import (
	"fmt"
	"io"
	"time"
	"unsafe"
)

// Decoder decodes one video, its pictures on the GPU.
type Decoder struct {
	d C.Dec
}

// Open reads the video at url, decoding with Direct3D 11 where it takes the
// codec.
func Open(url string, headers []string) (*Decoder, error) {
	cu := C.CString(url)
	defer C.free(unsafe.Pointer(cu))
	var ch *C.char
	if h := headerLines(headers); h != "" {
		ch = C.CString(h)
		defer C.free(unsafe.Pointer(ch))
	}
	p := &Decoder{}
	audioLog("open: dec_open begins")
	rc := C.dec_open(&p.d, cu, ch)
	audioLog(fmt.Sprintf("open: dec_open rc=%d", int(rc)))
	if rc != 0 {
		return nil, fmt.Errorf("gpudecode: open at step %d", int(rc))
	}
	audioLog("open: decoder made")
	return p, nil
}

// Size is the video's size in pixels.
func (p *Decoder) Size() (int, int) {
	return int(C.dec_width(&p.d)), int(C.dec_height(&p.d))
}

// Frame is a picture the decoder handed over.
type Frame struct {
	// Texture is the Direct3D 11 texture holding the picture, and Index the
	// slice of its array, when OnGPU is true.
	Texture unsafe.Pointer
	Index   int
	// OnGPU says the picture stayed on the GPU, which is the one worth
	// showing: the others came back through memory.
	OnGPU bool
	// PTS is the picture's timestamp, in the stream's time base units.
	PTS int64
	// Format is libavcodec's pixel format number, for a picture in memory.
	Format int
}

// Next decodes the next picture, or io.EOF after the last.
func (p *Decoder) Next() (Frame, error) {
	var tex unsafe.Pointer
	var index C.int
	var pts C.int64_t
	var format C.int
	rc := C.dec_next(&p.d, &tex, &index, &pts, &format)
	switch {
	case rc == 0:
		return Frame{}, io.EOF
	case rc > 0:
		return Frame{Texture: tex, Index: int(index), OnGPU: rc == 1, PTS: int64(pts), Format: int(format)}, nil
	default:
		return Frame{}, fmt.Errorf("gpudecode: frame at step %d", int(rc))
	}
}

// headerLines joins the headers the way libavformat takes them.
func headerLines(h []string) string {
	out := ""
	for _, line := range h {
		if line == "" {
			continue
		}
		if out != "" {
			out += "\r\n"
		}
		out += line
	}
	return out
}

// Picture converts the picture last decoded into the BGRA texture the
// renderer reads, and returns its NT handle and the texture's size.
func (p *Decoder) Picture() (uintptr, int, int, error) {
	var hnd C.uintptr_t
	var w, h C.int
	if rc := C.dec_picture(&p.d, &hnd, &w, &h); rc != 0 {
		return 0, 0, 0, fmt.Errorf("gpudecode: picture at step %d", int(rc))
	}
	return uintptr(hnd), int(w), int(h), nil
}

// FPS is the video's nominal rate.
func (p *Decoder) FPS() float64 { return float64(C.dec_fps(&p.d)) }

// Timebase is the stream's time base, in seconds a tick: a picture's PTS
// becomes seconds when multiplied by it.
func (p *Decoder) Timebase() float64 { return float64(C.dec_tb(&p.d)) }

// Duration is how long the video is.
func (p *Decoder) Duration() time.Duration {
	return time.Duration(int64(C.dec_duration_us(&p.d))) * time.Microsecond
}

// Seek goes to d in the video.
func (p *Decoder) Seek(d time.Duration) error {
	if rc := C.dec_seek_us(&p.d, C.int64_t(d.Microseconds())); rc < 0 {
		return fmt.Errorf("gpudecode: seek at step %d", int(rc))
	}
	return nil
}

// Close releases the decoder.
func (p *Decoder) Close() error { C.dec_free(&p.d); return nil }
