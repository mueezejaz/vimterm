package app

import (
	"strings"
	"testing"

	"vimterm/internal/emulator"
	"vimterm/internal/mode"
)

func TestStatusLine(t *testing.T) {
	row := make([]emulator.Cell, 40)
	statusLine(row, mode.ModeInsert, "", "powershell.exe", 5, 12, defaultStatusFg, defaultStatusBg, nil, 0)

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
	statusLine(row, mode.ModeNormal, "", "cmd.exe", 0, 0, fg, bg, nil, 0)
	for _, c := range row {
		if c.Fg != fg || c.Bg != bg {
			t.Errorf("cell has fg %+v bg %+v, want custom colors", c.Fg, c.Bg)
		}
	}
}

func TestStatusLineNormal(t *testing.T) {
	row := make([]emulator.Cell, 10)
	statusLine(row, mode.ModeNormal, "", "cmd.exe", 0, 0, defaultStatusFg, defaultStatusBg, nil, 0)
	if row[1].Content != "N" {
		t.Errorf("expected NORMAL indicator, got %q", row[1].Content)
	}
}

func TestStatusLineMessage(t *testing.T) {
	row := make([]emulator.Cell, 60)
	statusLine(row, mode.ModeNormal, "config reloaded", "cmd.exe", 0, 0, defaultStatusFg, defaultStatusBg, nil, 0)
	if row[8].Content != "c" {
		t.Errorf("message text missing after mode: %q", row[8].Content)
	}
}

func TestStatusLineUnicodeMessage(t *testing.T) {
	row := make([]emulator.Cell, 60)
	msg := "find: no 'é' forward"
	statusLine(row, mode.ModeNormal, msg, "cmd.exe", 0, 0, defaultStatusFg, defaultStatusBg, nil, 0)

	var sb strings.Builder
	for _, c := range row {
		sb.WriteString(c.Content)
	}
	text := sb.String()

	left := " NORMAL " + msg
	right := " cmd.exe  1,1 "
	if !strings.HasPrefix(text, left) {
		t.Errorf("unicode message misaligned, want prefix %q, got %q", left, text)
	}
	if !strings.HasSuffix(text, right) {
		t.Errorf("unicode message shifted right side, want suffix %q, got %q", right, text)
	}
	for i, r := range []rune(left) {
		if i >= len(row) {
			break
		}
		if row[i].Content != string(r) {
			t.Errorf("cell %d = %q, want %q", i, row[i].Content, string(r))
		}
	}
}

func TestStatusLineNarrowNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("statusLine panicked on narrow row: %v", r)
		}
	}()
	for cols := 1; cols < len(" NORMAL "); cols++ {
		row := make([]emulator.Cell, cols)
		statusLine(row, mode.ModeNormal, "", "powershell.exe", 3, 1, defaultStatusFg, defaultStatusBg, nil, 0)
	}
}

func TestStatusLineNarrowWithMessageNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("statusLine panicked on narrow row with message: %v", r)
		}
	}()
	for cols := 1; cols < len(" NORMAL ")+len("long transient message"); cols++ {
		row := make([]emulator.Cell, cols)
		statusLine(row, mode.ModeNormal, "long transient message", "powershell.exe", 3, 1, defaultStatusFg, defaultStatusBg, nil, 0)
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
