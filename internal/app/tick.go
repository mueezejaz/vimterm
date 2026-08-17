package app

import "time"

// newFrameTicker returns a channel that fires at ~30fps for coalesced
// repaints.
func newFrameTicker() <-chan time.Time {
	return time.Tick(33 * time.Millisecond)
}