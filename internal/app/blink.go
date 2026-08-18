package app

import "time"

// cursorBlinkPeriod is the on/off cadence of the virtual cursor while idle.
// The cursor stays solid for the first period after the last input, then
// toggles on the same cadence (a common terminal-style blink).
const cursorBlinkPeriod = 530 * time.Millisecond

// cursorBlinkPhase reports whether the virtual cursor should be visible at
// time now, given the time of the last user input. The cursor is solid for
// the first period after input, then alternates on/off.
func cursorBlinkPhase(now, lastInput time.Time) bool {
	idle := now.Sub(lastInput)
	if idle < 0 {
		idle = 0
	}
	return (idle/cursorBlinkPeriod)%2 == 0
}
