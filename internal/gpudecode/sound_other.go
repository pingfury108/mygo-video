//go:build !windows

package gpudecode

// lockSoundThread does nothing where the sound is not WASAPI: no platform of
// theirs ties a device to a system thread the way a COM apartment does.
func lockSoundThread() {}
