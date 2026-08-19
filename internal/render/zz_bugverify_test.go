package render

// Temporary bug-verification tests (to be removed after review).

import (
	"strings"
	"testing"

	"vimterm/internal/emulator"
)

// significant() treats a blank cell with an explicit #000000 background as
// styled when the fg is the terminal default (the only combination the
// emulator produces) — so the black-bg rendering path works.
func TestVerifySignificantBlackBg(t *testing.T) {
	black := emulator.Color{R: 0, G: 0, B: 0}
	def := emulator.Color{Default: true}
	// Emulator-sourced cell: Default fg, explicit black bg.
	c := emulator.Cell{Content: " ", Width: 1, Fg: def, Bg: black}
	if !significant(c) {
		t.Fatal("explicit black bg blank cell treated as insignificant (confirmed)")
	}
}

// Faithful black-tail scenario: emulator-sourced cells (Default fg), a
// full-screen app that paints every blank cell's background #000000. The
// blank black cells must be emitted so the row tail is black, not the
// terminal default.
func TestVerifyDrawBlackBgTail(t *testing.T) {
	black := emulator.Color{R: 0, G: 0, B: 0}
	def := emulator.Color{Default: true}
	f := NewFrame(10, 1)
	f.Cells[0][0] = emulator.Cell{Content: "h", Width: 1, Fg: def, Bg: def}
	f.Cells[0][1] = emulator.Cell{Content: "i", Width: 1, Fg: def, Bg: def}
	for x := 2; x < 10; x++ {
		f.Cells[0][x] = emulator.Cell{Content: " ", Width: 1, Fg: def, Bg: black}
	}
	r := New()
	var sb strings.Builder
	r.Draw(&sb, f)
	out := sb.String()
	t.Logf("draw: %q", out)
	if !strings.Contains(out, "48;2;0;0;0") {
		t.Fatal("black bg not emitted: row tail erased with default bg (confirmed)")
	}
}
