//go:build windows

// Command syncprobe plays a video in a real window — d3d11 present, WASAPI and
// all — and records what the output endpoint plays while it does. The engine's
// frame log says when each picture reached the screen; this says when each
// sound left the device. Lined up against the same clock, that is the sync the
// ear hears, which no clock inside the playback can report.
//
//	syncprobe.exe <url or file> [seconds [startSeconds]]
//
// It prints the recording's path and the moment it began, which the frame log's
// own times are compared against.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-video/internal/gpudecode"
	"mygo-video/internal/looprec"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: syncprobe.exe <url or file> [seconds [startSeconds]]")
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
	for _, name := range []string{"mygo-sync.log", "mygo-frames.log"} {
		os.Remove(filepath.Join(os.TempDir(), name))
	}
	os.WriteFile(filepath.Join(os.TempDir(), "mygo-trace"), []byte("x"), 0o600)

	mygo.App.WhenReady(func() {
		e, err := gpudecode.NewEngine(url, nil)
		if err != nil {
			fmt.Println("open:", err)
			os.Exit(1)
		}
		if start > 0 {
			e.Seek(time.Duration(start) * time.Second)
		}
		pl := ui.NewVideoPlayerOf(e)
		pl.Play()
		var win *mygo.Window
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:  "MyGo Video syncprobe",
			Width:  960,
			Height: 540,
			Content: ui.View(func(c *ui.Context) {
				c.OnShortcut(0, ui.KeyEscape, func() { win.Close() })
				ui.Video(c, pl)
			}),
		})
		// The brightness of the screen, taken here beside the recording and on
		// the same clock: the picture of a check flashes white, and its moment
		// written down is what the sound's moment is measured against.
		go func() {
			path := filepath.Join(os.TempDir(), "syncprobe-luma.txt")
			f, err := os.Create(path)
			if err != nil {
				return
			}
			defer f.Close()
			end := time.Now().Add(time.Duration(secs) * time.Second)
			for time.Now().Before(end) {
				t := time.Now()
				if v := luma(); v >= 0 {
					fmt.Fprintf(f, "%.4f %d\n", float64(t.UnixNano())/1e9, v)
				}
				time.Sleep(8 * time.Millisecond)
			}
		}()
		// The recording starts with the picture, and both are timed off the
		// same clock: the log of the frames and the samples of the sound.
		go func() {
			clip, err := looprec.Record(looprec.Temp("syncprobe.wav"), time.Duration(secs)*time.Second)
			if err != nil {
				fmt.Println("record:", err)
				os.Exit(1)
			}
			fmt.Printf("recording %s started %s %.3f rate=%d channels=%d\n",
				clip.Path, clip.Started.Format("2006-01-02T15:04:05.000"),
				float64(clip.Started.UnixNano())/1e9, clip.Rate, clip.Channels)
			os.Exit(0)
		}()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Println("run:", err)
		os.Exit(1)
	}
}
