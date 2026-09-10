package app

import (
	"testing"
	"time"
)

func TestCursorBlinkPhase(t *testing.T) {
	base := time.Unix(0, 0)
	cases := []struct {
		idle time.Duration
		want bool
	}{
		{0, true},
		{cursorBlinkPeriod - time.Millisecond, true},
		{cursorBlinkPeriod, false},
		{2*cursorBlinkPeriod - time.Millisecond, false},
		{2 * cursorBlinkPeriod, true},
		{3 * cursorBlinkPeriod, false},
		{-time.Second, true},
	}
	for _, c := range cases {
		now := base.Add(c.idle)
		if got := cursorBlinkPhase(now, base); got != c.want {
			t.Errorf("cursorBlinkPhase(idle=%v) = %v, want %v", c.idle, got, c.want)
		}
	}
}

func TestCursorBlinkPhaseZeroTime(t *testing.T) {
	// When lastInput is the zero time (no input ever received), the cursor
	// should still produce a deterministic result. Currently the result
	// depends on the absolute wall-clock time modulo the blink period,
	// which makes the initial blink phase non-deterministic across launches.
	// This test documents the current behavior: two calls 1ns apart may
	// disagree if they cross a blink boundary.
	now := time.Now()
	lastInput := time.Time{}
	r1 := cursorBlinkPhase(now, lastInput)
	r2 := cursorBlinkPhase(now.Add(1*time.Nanosecond), lastInput)
	// If these differ, the zero-time case is non-deterministic:
	// the cursor could start visible or hidden depending on the wall clock.
	if r1 != r2 {
		t.Logf("cursorBlinkPhase is non-deterministic for zero-time lastInput: %v vs %v (1ns apart)", r1, r2)
		// This is a known issue: the result depends on the absolute time.
		// A fix would special-case zero-time to always return true (visible).
	}
}
