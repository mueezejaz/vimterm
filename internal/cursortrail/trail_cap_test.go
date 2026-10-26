package cursortrail

// Regression test for the ring invariant at trailMaxEntries: reserve used to
// leave count == len(buf) untouched when the oldest entry was still alive and
// the buffer was at the hard cap, so appendEntry overwrote the front entry
// while still counting it — count outgrew the backing array and Ghosts
// emitted the same entry multiple times.

import (
	"testing"
	"time"
)

func TestRingDoesNotDuplicateAtHardCap(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     time.Hour, // nothing expires: the oldest entry stays alive
		MaxPositions: 4,
	})
	now := time.Now()
	for i := 0; i < trailMaxEntries+64; i++ {
		trail.Record(i, 0, now)
	}
	ghosts := trail.Ghosts(now)
	if len(ghosts) > trailMaxEntries {
		t.Fatalf("%d ghosts from a %d-entry ring: count outgrew the buffer",
			len(ghosts), trailMaxEntries)
	}
	seen := make(map[[2]int]bool, len(ghosts))
	for _, g := range ghosts {
		key := [2]int{g.X, g.Line}
		if seen[key] {
			t.Fatalf("duplicate ghost at (%d,%d): ring invariant broken", g.X, g.Line)
		}
		seen[key] = true
	}
}
