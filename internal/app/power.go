package app

import "github.com/egoist/mygo"

const displayWakeReason = "MyGo Video playback"

// keepDisplayOn asks the platform to keep the screen on while a video is
// playing. The release is held across frames, so a pause can clear and
// playing again can set the same request without a power-system churn.
func (a *app) keepDisplayOn() {
	if a.displayLock == nil {
		a.displayLock = mygo.Power.KeepAwake(displayWakeReason, true)
	}
	a.displaySeen = true
}

// releaseDisplayLock gives the display back to the system's idle timer.
// It stays ready for the next playback until the player itself is closed.
func (a *app) releaseDisplayLock() {
	a.displaySeen = false
	if !a.playingDisplay() && a.displayLock != nil {
		release := a.displayLock
		a.displayLock = nil
		release()
	}
}

// playingDisplay reports whether anything that keeps a screen on still
// belongs to the app: the built-in player, or mpv in its own window.
func (a *app) playingDisplay() bool {
	return (a.player != nil && a.player.Playing()) || a.mpvPlaying
}
