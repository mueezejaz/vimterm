package app

import (
	"strings"

	"vimterm/internal/emulator"
	"vimterm/internal/mode"
)

// Default status line colors (used until overridden by [colors]).
var (
	defaultStatusFg = emulator.Color{R: 235, G: 235, B: 255}
	defaultStatusBg = emulator.Color{R: 40, G: 40, B: 90}
)

// statusLine fills the given row with the status line: the mode name on the
// left (plus any transient message), the open tabs in the middle (only when
// more than one is open; the active label is reversed), and shell/cursor
// information on the right.
func statusLine(row []emulator.Cell, m mode.Mode, msg, shell string, cx, cy int, fg, bg emulator.Color, labels []string, active int) {
	// Build left string without fmt.Sprintf.
	var left strings.Builder
	left.WriteByte(' ')
	left.WriteString(m.String())
	left.WriteByte(' ')
	if msg != "" {
		left.WriteString(msg)
	}

	// Build right string without fmt.Sprintf.
	var right strings.Builder
	right.WriteByte(' ')
	right.WriteString(shell)
	right.WriteString("  ")
	right.WriteString(itoa(cy + 1))
	right.WriteByte(',')
	right.WriteString(itoa(cx + 1))
	right.WriteByte(' ')

	leftStr := left.String()
	rightStr := right.String()
	leftRunes := []rune(leftStr)
	rightRunes := []rune(rightStr)

	for i := range row {
		row[i] = emulator.Cell{Content: " ", Width: 1, Fg: fg, Bg: bg}
	}
	for i, r := range leftRunes {
		if i >= len(row) {
			break
		}
		row[i] = statusCell(r, fg, bg)
	}
	rightStart := len(row) - len(rightRunes)
	if rightStart < 0 {
		rightStart = 0
	}
	if leftLen := len(leftRunes); leftLen < len(row) && rightStart < leftLen {
		rightStart = leftLen
	}
	for i, r := range rightRunes {
		col := rightStart + i
		if col >= len(row) {
			break
		}
		row[col] = statusCell(r, fg, bg)
	}
	drawTabLabels(row, labels, active, len(leftRunes), rightStart, fg, bg)
}

// drawTabLabels writes the tab labels centered in the gap between the left
// text (ending at leftEnd) and the right text (starting at rightStart).
// Labels are dropped whole from the end when space runs out and the
// remainder is centered in the leftover space; the active tab's label is
// drawn reversed. With fewer than two labels it draws nothing.
func drawTabLabels(row []emulator.Cell, labels []string, active, leftEnd, rightStart int, fg, bg emulator.Color) {
	if len(labels) < 2 {
		return
	}
	if rightStart-leftEnd < 6 {
		// Not enough room to show anything useful.
		return
	}
	// Drop trailing labels until the padded block fits between the left
	// and right text.
	total := 0
	shown := 0
	for i, l := range labels {
		w := 1 + len([]rune(l)) + 1 // padding + label + padding
		if leftEnd+total+w > rightStart-1 {
			break
		}
		total += w
		shown = i + 1
	}
	start := leftEnd + (rightStart-leftEnd-total)/2
	pos := start
	for i := 0; i < shown; i++ {
		row[pos] = statusCell(' ', fg, bg)
		pos++
		for _, r := range []rune(labels[i]) {
			c := statusCell(r, fg, bg)
			c.Reverse = i == active
			row[pos] = c
			pos++
		}
		row[pos] = statusCell(' ', fg, bg)
		pos++
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
	var left strings.Builder
	left.WriteByte(' ')
	left.WriteString(m.String())
	left.WriteByte(' ')
	left.WriteString(msg)
	left.WriteByte(' ')
	for i, r := range []rune(left.String()) {
		if i >= len(row) {
			return
		}
		row[i] = statusCell(r, fg, bg)
	}
}
