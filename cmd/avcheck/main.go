//go:build windows

// Command avcheck plays a video the way the app does and reports, every second,
// where the sound and the picture stand against the clock: the check of the
// sync that takes no one clicking.
//
//	avcheck.exe <url or file> [seconds]
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"mygo-video/internal/gpudecode"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: avcheck.exe <url or file> [seconds]")
		return
	}
	url := os.Args[1]
	secs := 15
	if len(os.Args) > 2 {
		if n, err := strconv.Atoi(os.Args[2]); err == nil {
			secs = n
		}
	}
	start := 0
	if len(os.Args) > 3 {
		if n, err := strconv.Atoi(os.Args[3]); err == nil {
			start = n
		}
	}
	e, err := gpudecode.NewEngine(url, nil)
	if err != nil {
		fmt.Println("open failed:", err)
		return
	}
	defer e.Close()
	if start > 0 {
		e.Seek(time.Duration(start) * time.Second)
	}
	e.Play()
	fmt.Printf("playing %s for %ds from %ds\n", url, secs, start)
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	next := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		e.Frame(time.Now())
		time.Sleep(16 * time.Millisecond)
		if time.Now().After(next) {
			next = next.Add(time.Second)
			s := e.Stat(time.Now())
			drift := s.Audio - s.Real
			fmt.Printf("real=%6.2fs audio=%6.2fs video=%6.2fs drift=%+7.3fs queue=%d state=%d err=%v\n",
				s.Real.Seconds(), s.Audio.Seconds(), s.Video.Seconds(), drift.Seconds(), s.Queue, s.State, s.Err)
		}
	}
}
