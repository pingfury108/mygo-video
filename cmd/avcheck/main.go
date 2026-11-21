//go:build windows

// Command avcheck plays a video the way the app does and reports, every second,
// where the sound and the picture stand against the clock: the check of the
// sync that takes no one clicking.
//
//	avcheck.exe <url or file> [seconds [startSeconds [seekEvery]]]
//
// seekEvery seeks the playback as it runs, which is the drag of the bar in the
// app, and harder on the decoder than a single seek before the start.
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
	seekEvery := 0
	if len(os.Args) > 4 {
		if n, err := strconv.Atoi(os.Args[4]); err == nil {
			seekEvery = n
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
	nextSeek := time.Now().Add(time.Duration(seekEvery) * time.Second)
	nextGap := time.Now().Add(100 * time.Millisecond)
	where := time.Duration(start) * time.Second
	for time.Now().Before(deadline) {
		e.Frame(time.Now())
		time.Sleep(16 * time.Millisecond)
		if seekEvery > 0 && time.Now().After(nextSeek) {
			nextSeek = nextSeek.Add(time.Duration(seekEvery) * time.Second)
			where += time.Duration(seekEvery)*time.Second + 90*time.Second
			e.Seek(where)
			fmt.Printf("seek to %v\n", where)
		}
		if time.Now().After(next) {
			next = next.Add(time.Second)
			s := e.Stat(time.Now())
			drift := s.Audio - s.Real
			fmt.Printf("real=%6.2fs audio=%6.2fs video=%6.2fs drift=%+7.3fs queue=%d state=%d err=%v\n",
				s.Real.Seconds(), s.Audio.Seconds(), s.Video.Seconds(), drift.Seconds(), s.Queue, s.State, s.Err)
		}
		// A fine look at the gap: a sawtooth from zero to one frame is the
		// picture waiting for its moment; a level gap is an offset.
		if os.Getenv("GAPS") != "" && time.Now().After(nextGap) {
			nextGap = nextGap.Add(100 * time.Millisecond)
			v, a, ok := e.Sync()
			if ok {
				fmt.Printf("  gap=%+5dms shown\n", (v - a).Milliseconds())
			}
		}
	}
}
