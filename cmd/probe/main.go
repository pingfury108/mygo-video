//go:build windows

// Command probe plays a video in a real window of its own — the full way of
// the app, d3d11 present and DWM composition and all — and leaves the engine's
// own logs to be read: what a present took to be shown is measured only with
// the whole pipeline standing, and this stands it without anyone clicking.
//
//	probe.exe <url or file> [seconds]
//
// It says where the logs are when it ends. Run it in the session that shows
// the desktop (a task of schtasks /it from a service session): the compositor
// is not in a service session.
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
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: probe.exe <url or file> [seconds]")
		return
	}
	url := os.Args[1]
	secs := 20
	if len(os.Args) > 2 {
		if n, err := strconv.Atoi(os.Args[2]); err == nil {
			secs = n
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
		pl := ui.NewVideoPlayerOf(e)
		pl.Play()
		var win *mygo.Window
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:  "MyGo Video probe",
			Width:  1280,
			Height: 720,
			Content: ui.View(func(c *ui.Context) {
				c.OnShortcut(0, ui.KeyEscape, func() { win.Close() })
				ui.Video(c, pl)
			}),
		})
		time.AfterFunc(time.Duration(secs)*time.Second, func() { os.Exit(0) })
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Println("run:", err)
		os.Exit(1)
	}
}
