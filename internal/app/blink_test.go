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
