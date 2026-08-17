package app

import (
	"strings"
	"testing"

	"vimterm/internal/emulator"
	"vimterm/internal/mode"
)

func TestStatusLine(t *testing.T) {
	row := make([]emulator.Cell, 40)
	statusLine(row, mode.ModeInsert, "", "powershell.exe", 5, 12, defaultStatusFg, defaultStatusBg)

	var sb strings.Builder
	for _, c := range row {
		sb.WriteString(c.Content)
	}
	text := sb.String()

	if !strings.HasPrefix(text, " INSERT ") {
		t.Errorf("mode indicator missing: %q", text)
	}
	if !strings.HasSuffix(text, " powershell.exe  13,6 ") {
		t.Errorf("right side missing: %q", text)
	}
	for _, c := range row {
		if c.Bg != defaultStatusBg {
			t.Errorf("cell %q has bg %+v, want status bg", c.Content, c.Bg)
		}
	}
}

func TestStatusLineCustomColors(t *testing.T) {
	row := make([]emulator.Cell, 10)
	fg := emulator.Color{R: 1, G: 2, B: 3}
	bg := emulator.Color{R: 4, G: 5, B: 6}
	statusLine(row, mode.ModeNormal, "", "cmd.exe", 0, 0, fg, bg)
	for _, c := range row {
		if c.Fg != fg || c.Bg != bg {
			t.Errorf("cell has fg %+v bg %+v, want custom colors", c.Fg, c.Bg)
		}
	}
}

func TestStatusLineNormal(t *testing.T) {
	row := make([]emulator.Cell, 10)
	statusLine(row, mode.ModeNormal, "", "cmd.exe", 0, 0, defaultStatusFg, defaultStatusBg)
	if row[1].Content != "N" {
		t.Errorf("expected NORMAL indicator, got %q", row[1].Content)
	}
}

func TestStatusLineMessage(t *testing.T) {
	row := make([]emulator.Cell, 60)
	statusLine(row, mode.ModeNormal, "config reloaded", "cmd.exe", 0, 0, defaultStatusFg, defaultStatusBg)
	if row[8].Content != "c" {
		t.Errorf("message text missing after mode: %q", row[8].Content)
	}
}

func TestTerminalRows(t *testing.T) {
	cases := map[int]int{0: 1, 1: 1, 2: 1, 24: 23, 30: 29}
	for in, want := range cases {
		if got := terminalRows(in); got != want {
			t.Errorf("terminalRows(%d) = %d, want %d", in, got, want)
		}
	}
}