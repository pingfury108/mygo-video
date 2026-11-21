//go:build !windows

package gpudecode

// waitVsync returns at once: no compositor to line the pictures up with here.
func waitVsync() {}
