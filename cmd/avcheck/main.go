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
	// BUSY spreads the goroutines over more system threads, as the app's own
	// do: a sound device reaches WASAPI from whichever thread runs it.
	if n, err := strconv.Atoi(os.Getenv("BUSY")); err == nil && n > 0 {
		for i := 0; i < n; i++ {
			go func() {
				for {
					time.Sleep(200 * time.Microsecond)
				}
			}()
		}
		fmt.Printf("busy: %d goroutines\n", n)
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
	back := false
	// DRAGS limits the seeks to a hand that drags a while and stops: what
	// the playback does after the hand is off the bar is the question, and
	// a check that keeps dragging never asks it.
	drags, _ := strconv.Atoi(os.Getenv("DRAGS"))
	seeks := 0
	for time.Now().Before(deadline) {
		e.Frame(time.Now())
		time.Sleep(16 * time.Millisecond)
		if seekEvery > 0 && time.Now().After(nextSeek) && (drags == 0 || seeks < drags) {
			// FAST drags the bar every half second, which is a hand moving
			// it rather than a click on a place: the seeks come faster than
			// the keyframe of one decodes, and each may land while the last
			// has not reached the decoder yet.
			seekGap := time.Duration(seekEvery) * time.Second
			if os.Getenv("FAST") != "" {
				seekGap = 500 * time.Millisecond
			}
			seeks++
			nextSeek = nextSeek.Add(seekGap)
			// BACK drags the bar forward and back in turn, which is the hand
			// on a film: a seek forward alone leaves the device seeked one
			// way, and a place walked back to is where a stale clock shows.
			if os.Getenv("BACK") != "" {
				if back {
					where -= 120 * time.Second
				} else {
					where += 150 * time.Second
				}
				back = !back
			} else {
				where += time.Duration(seekEvery)*time.Second + 90*time.Second
			}
			if where < 0 {
				where = 0
			}
			e.Seek(where)
			fmt.Printf("seek to %v\n", where)
		}
		if time.Now().After(next) {
			next = next.Add(time.Second)
			s := e.Stat(time.Now())
			// The difference of the two places is the sync itself: a picture
			// standing ahead of the sound shows sound that has not been heard.
			drift := s.Video - s.Audio
			fmt.Printf("real=%6.2fs audio=%8.2fs video=%8.2fs drift=%+8.3fs queue=%d state=%d err=%v\n",
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
