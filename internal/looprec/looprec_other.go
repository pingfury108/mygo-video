//go:build !windows

// Package looprec records what the default output endpoint plays, for the
// check of the sync that hears the sound. Only Windows has the device clock
// this reads.
package looprec

import (
	"errors"
	"time"
)

// Clip is what a recording holds.
type Clip struct {
	Path     string
	Rate     int
	Channels int
	Bits     int
	Started  time.Time
}

// Record is not there off Windows.
func Record(path string, d time.Duration) (Clip, error) {
	return Clip{}, errors.New("looprec: only Windows records the output")
}

// Temp returns a path under the temporary directory for a recording.
func Temp(name string) string { return name }
