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
	for i, r := range left {
		if i >= len(row) {
			break
		}
		row[i] = statusCell(r, fg, bg)
	}
	rightStart := len(row) - len(right)
	for i, r := range right {
		if rightStart+i < 0 {
			break
		}
		row[rightStart+i] = statusCell(r, fg, bg)
	}
}

func statusCell(r rune, fg, bg emulator.Color) emulator.Cell {
	return emulator.Cell{Content: string(r), Width: 1, Fg: fg, Bg: bg, Bold: true}
}