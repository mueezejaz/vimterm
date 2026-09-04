package app

import "time"

// newFrameTicker returns a ticker that fires at ~60fps for coalesced
// repaints — fast enough that the cursor trail's fade and comet animate
// smoothly. The loop only repaints when something is dirty, so idle cost is
// just the per-tick dirty checks. The caller must call Stop when the loop
// exits.
func newFrameTicker() *time.Ticker {
	return time.NewTicker(16 * time.Millisecond)
}
