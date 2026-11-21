//go:build !windows

package gpudecode

import "time"

// waitVsync returns at once: no compositor to line the pictures up with here.
func waitVsync() {}

// composePeriod is 0: this platform's compositor is not waited on.
func composePeriod() time.Duration { return 0 }
