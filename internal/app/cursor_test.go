package app

import (
	"testing"

	"vimterm/internal/emulator"
)

func TestCursorMoveSeq(t *testing.T) {
	cases := []struct {
		delta int
		want  string
	}{
		{0, ""},
		{1, "\x1b[D"},
		{3, "\x1b[D\x1b[D\x1b[D"},
		{-1, "\x1b[C"},
		{-2, "\x1b[C\x1b[C"},
	}
	for _, c := range cases {
		if got := string(cursorMoveSeq(c.delta)); got != c.want {
			t.Errorf("cursorMoveSeq(%d) = %q, want %q", c.delta, got, c.want)
		}
	}
}

func TestCursorBlockStyle(t *testing.T) {
	themeFg := emulator.Color{R: 235, G: 235, B: 255}
	themeBg := emulator.Color{R: 15, G: 15, B: 30}
	red := emulator.Color{R: 255, G: 0, B: 0}

	cases := []struct {
		name           string
		cell           emulator.Cell
		wantFg, wantBg emulator.Color
	}{
		{
			"plain cell inverts the theme",
			emulator.Cell{},
			themeBg, themeFg,
		},
		{
			"highlighted cell lands on the opposite pair of the highlight",
			emulator.Cell{Reverse: true},
			themeFg, themeBg,
		},
		{
			"colored text keeps a distinct block",
			emulator.Cell{Fg: red},
			themeBg, red,
		},
		{
			"highlighted colored text un-reverses",
			emulator.Cell{Fg: red, Reverse: true},
			red, themeBg,
		},
	}
	for _, c := range cases {
		fg, bg := cursorBlockStyle(c.cell, themeFg, themeBg)
		if fg != c.wantFg || bg != c.wantBg {
			t.Errorf("%s: cursorBlockStyle = fg %+v bg %+v, want fg %+v bg %+v",
				c.name, fg, bg, c.wantFg, c.wantBg)
		}
	}
}
