//go:build windows

package gpudecode

import (
	"sync"
	"syscall"
	"time"
)

// The system's clock ticks 64 times a second unless asked otherwise: a timer
// comes no finer than that, so a window asking for a frame every 16.7ms is
// served at 31ms — two ticks — and the pictures land 31ms apart where the film
// steps every 40. mpv asks for the finer clock on Windows for the same reason
// (osdep/timer-win32.c), and every player of video here has to.
var (
	winmm           = syscall.NewLazyDLL("winmm.dll")
	timeBeginPeriod = winmm.NewProc("timeBeginPeriod")
)

func init() {
	if timeBeginPeriod.Find() == nil {
		timeBeginPeriod.Call(1) // a millisecond, the finest Windows offers
	}
}

// dwmFlush waits for the desktop compositor's next refresh: a frame handed
// over right after it reaches the screen at the refresh that follows, while
// one handed over at a moment of its own waits for whichever refresh comes
// next, and the pictures land 31ms and 61ms apart where the film steps every
// 40. Sleeping alone is not enough to line them up: a sleep on Windows keeps
// to within a millisecond or two, and that is what the presents drift by.
var dwmFlush = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmFlush")

// The compositor's own period, measured once: a desktop sent over a remote
// session composes at 32Hz whatever the display reports, and waiting on that
// refresh costs a picture more than it lines it up.
var (
	composeOnce sync.Once
	composeStep time.Duration
)

// composePeriod measures the compositor's refresh by waiting on it a few times,
// 0 when it cannot be waited on.
func composePeriod() time.Duration {
	composeOnce.Do(func() {
		if dwmFlush.Find() != nil {
			return
		}
		dwmFlush.Call()
		var sum time.Duration
		for i := 0; i < 6; i++ {
			start := time.Now()
			dwmFlush.Call()
			sum += time.Since(start)
		}
		composeStep = sum / 6
	})
	return composeStep
}

// waitVsync returns just after the compositor drew a frame, where the
// compositor keeps the display's pace: a remote session composes at 32Hz, and
// waiting for that only holds a picture back.
func waitVsync() {
	step := composePeriod()
	if step == 0 || step > 25*time.Millisecond {
		return
	}
	dwmFlush.Call()
}
