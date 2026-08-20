package app

import (
	"fmt"

	"vimterm/internal/emulator"
	"vimterm/internal/mode"
)

// Default status line colors (used until overridden by [colors]).
var (
	defaultStatusFg = emulator.Color{R: 235, G: 235, B: 255}
	defaultStatusBg = emulator.Color{R: 40, G: 40, B: 90}
)

// statusLine fills the given row with the status line: the mode name on the
// left (plus any transient message), and shell/cursor information on the
// right.
func statusLine(row []emulator.Cell, m mode.Mode, msg, shell string, cx, cy int, fg, bg emulator.Color) {
	left := fmt.Sprintf(" %s ", m)
	if msg != "" {
		left += msg
	}
	right := fmt.Sprintf(" %s  %d,%d ", shell, cy+1, cx+1)

	for i := range row {
		row[i] = emulator.Cell{Content: " ", Width: 1, Fg: fg, Bg: bg}
	}
	leftRunes := []rune(left)
	for i, r := range leftRunes {
		if i >= len(row) {
			break
		}
		row[i] = statusCell(r, fg, bg)
	}
	rightRunes := []rune(right)
	rightStart := len(row) - len(rightRunes)
	if rightStart < 0 {
		// The row is narrower than the right text; start at the first column
		// so the loop below truncates instead of indexing out of range.
		rightStart = 0
	}
	if leftLen := len(leftRunes); leftLen < len(row) && rightStart < leftLen {
		// The right side would overlap the mode indicator: clamp it to the
		// columns right of the left text so narrow terminals keep both.
		rightStart = leftLen
	}
	for i, r := range rightRunes {
		col := rightStart + i
		if col >= len(row) {
			break
		}
		row[col] = statusCell(r, fg, bg)
	}
}

func statusCell(r rune, fg, bg emulator.Color) emulator.Cell {
	return emulator.Cell{Content: string(r), Width: 1, Fg: fg, Bg: bg, Bold: true}
}

// mergeAllowed reports whether vimterm's status message may be overlaid on
// the child's bottom row (a full-screen app's status line). "always" allows
// it unconditionally, "never" forbids it, and "auto" (the default) requires
// the row to look like a status line: cells with an explicit background
// color, as nvim status lines have (plain rows of less/top do not).
func mergeAllowed(row []emulator.Cell, mode string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	}
	for _, c := range row {
		if c.Bg.Default {
			continue
		}
		return true
	}
	return false
}

// overlayStatusMessage composes the merged bottom row while a full-screen
// app owns the screen: the mode name and the transient message replace the
// left edge of the row, and the rest of the app's status line stays
// untouched.
func overlayStatusMessage(row []emulator.Cell, m mode.Mode, msg string, fg, bg emulator.Color) {
	left := fmt.Sprintf(" %s %s ", m, msg)
	for i, r := range []rune(left) {
		if i >= len(row) {
			return
		}
		row[i] = statusCell(r, fg, bg)
	}
}