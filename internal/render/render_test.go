package render

import (
	"strings"
	"testing"

	"vimterm/internal/emulator"
)

func TestDraw(t *testing.T) {
	def := emulator.Color{Default: true}
	f := NewFrame(4, 2)
	f.Cells[0][0] = emulator.Cell{Content: "h", Width: 1, Fg: def, Bg: def}
	f.Cells[0][1] = emulator.Cell{Content: "i", Width: 1, Fg: def, Bg: def}
	f.Cells[1][0] = emulator.Cell{Content: "x", Width: 1, Fg: emulator.Color{R: 255}, Bg: def, Bold: true}
	f.CursorX, f.CursorY = 2, 1

	var sb strings.Builder
	Draw(&sb, f)
	out := sb.String()

	if !strings.Contains(out, "\x1b[?25l") {
		t.Error("draw must hide the cursor first")
	}
	if !strings.Contains(out, "\x1b[?25h") {
		t.Error("draw must show the cursor")
	}
	if !strings.Contains(out, "\x1b[1;1H\x1b[0mhi\x1b[0m\x1b[K") {
		t.Errorf("row 1 missing: %q", out)
	}
	if !strings.Contains(out, "\x1b[2;1H\x1b[0m\x1b[1;38;2;255;0;0mx\x1b[0m") {
		t.Errorf("row 2 style/content missing: %q", out)
	}
	if !strings.HasSuffix(out, "\x1b[2;3H\x1b[?25h") {
		t.Errorf("cursor must be placed at 2;3, got: %q", out)
	}
}

func TestDrawWideChar(t *testing.T) {
	f := NewFrame(4, 1)
	// Wide char '中' followed by a zero placeholder cell.
	f.Cells[0][0] = emulator.Cell{Content: "中", Width: 2}
	f.Cells[0][1] = emulator.Cell{Width: 0}
	f.Cells[0][2] = emulator.Cell{Content: "a", Width: 1}

	var sb strings.Builder
	Draw(&sb, f)
	out := sb.String()

	if !strings.Contains(out, "中a") {
		t.Errorf("wide char content missing: %q", out)
	}
	if !strings.Contains(out, "\x1b[K") {
		t.Errorf("line must be erased to end: %q", out)
	}
}

func TestDrawKeepsCursorOnEmptySpace(t *testing.T) {
	def := emulator.Color{Default: true}
	f := NewFrame(10, 1)
	// Text ends at col 2; the virtual cursor sits at col 6, on blank space.
	f.Cells[0][0] = emulator.Cell{Content: "a", Width: 1, Fg: def, Bg: def}
	f.Cells[0][1] = emulator.Cell{Content: "b", Width: 1, Fg: def, Bg: def}
	f.Cells[0][2] = emulator.Cell{Content: "c", Width: 1, Fg: def, Bg: def}
	f.Cells[0][6] = emulator.Cell{Content: " ", Width: 1, Reverse: true, Fg: def, Bg: def}

	var sb strings.Builder
	Draw(&sb, f)
	out := sb.String()

	if !strings.Contains(out, "\x1b[7m ") {
		t.Errorf("cursor on empty space must be drawn, not erased: %q", out)
	}
}

func TestDrawKeepsSelectionOnEmptyLine(t *testing.T) {
	def := emulator.Color{Default: true}
	f := NewFrame(10, 1)
	// An entirely blank row where a line-wise selection reverses cols 2..7.
	for x := 2; x <= 7; x++ {
		f.Cells[0][x] = emulator.Cell{Content: " ", Width: 1, Reverse: true, Fg: def, Bg: def}
	}

	var sb strings.Builder
	Draw(&sb, f)
	out := sb.String()

	if !strings.Contains(out, "\x1b[7m ") {
		t.Errorf("selection on empty line must be drawn, not erased: %q", out)
	}
	if !strings.Contains(out, "\x1b[K") {
		t.Errorf("cells after the selection must be erased: %q", out)
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 1: "1", 9: "9", 10: "10", 100: "100", 1000: "1000"}
	for n, want := range cases {
		if got := itoa(n); got != want {
			t.Errorf("itoa(%d) = %q, want %q", n, got, want)
		}
	}
}