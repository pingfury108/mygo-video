//go:build windows

package gpudecode

import "syscall"

// dwmFlush waits for the desktop compositor's next refresh: a frame handed
// over right after it reaches the screen at the refresh that follows, while
// one handed over at a moment of its own waits for whichever refresh comes
// next, and the pictures land 31ms and 61ms apart where the film steps every
// 40. Sleeping alone is not enough to line them up: a sleep on Windows keeps
// to within a millisecond or two, and that is what the presents drift by.
var dwmFlush = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmFlush")

// waitVsync returns just after the compositor drew a frame.
func waitVsync() {
	if dwmFlush.Find() == nil {
		dwmFlush.Call()
	}
}
