package app

import "time"

// newFrameTicker returns a ticker that fires at ~30fps for coalesced
// repaints. The caller must call Stop when the loop exits.
func newFrameTicker() *time.Ticker {
	return time.NewTicker(33 * time.Millisecond)
}