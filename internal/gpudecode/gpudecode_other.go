//go:build !windows

package gpudecode

import (
	"errors"
	"time"
)

// ErrUnsupported says the GPU decoder is not wired on this platform.
var ErrUnsupported = errors.New("gpudecode: not wired here")

// Decoder decodes one video.
type Decoder struct{}

// Frame is a picture.
type Frame struct {
	Texture uintptr
	Index   int
	OnGPU   bool
	PTS     int64
	Format  int
}

// Open reports that the GPU decoder is not wired here.
func Open(url string, headers []string) (*Decoder, error) { return nil, ErrUnsupported }

func (p *Decoder) Size() (int, int)     { return 0, 0 }
func (p *Decoder) Next() (Frame, error) { return Frame{}, ErrUnsupported }

var _ = time.Second

// Picture reports that the GPU decoder is not wired here.
func (p *Decoder) Picture() (uintptr, int, int, error) { return 0, 0, 0, ErrUnsupported }

// FPS is the video's nominal rate.
func (p *Decoder) FPS() float64 { return 25 }

// Duration is how long the video is.
func (p *Decoder) Duration() time.Duration { return 0 }

// Seek goes to d in the video.
func (p *Decoder) Seek(d time.Duration) error { return ErrUnsupported }

// Close releases the decoder.
func (p *Decoder) Close() error { return nil }
