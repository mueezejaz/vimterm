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

func TestDrawEmitsExplicitBlackOnBlack(t *testing.T) {
	// A blank cell whose foreground AND background are explicitly black is
	// styled content: it must be emitted, not treated as an empty tail.
	black := emulator.Color{}
	if !significant(emulator.Cell{Content: " ", Width: 1, Fg: black, Bg: black}) {
		t.Fatal("explicit black-on-black cell reported as insignificant")
	}
	f := NewFrame(6, 1)
	f.Cells[0][2] = emulator.Cell{Content: " ", Width: 1, Fg: black, Bg: black}

	var sb strings.Builder
	Draw(&sb, f)
	out := sb.String()

	if !strings.Contains(out, "38;2;0;0;0") || !strings.Contains(out, "48;2;0;0;0") {
		t.Errorf("explicit black colors not emitted: %q", out)
	}
}

func TestNewFrameDefaultsAreUnset(t *testing.T) {
	// Untouched frame cells carry the terminal default (not the zero value),
	// so they stay insignificant while explicit black does not.
	f := NewFrame(4, 1)
	if significant(f.Cells[0][0]) {
		t.Fatal("untouched default cell reported as significant")
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 1: "1", 9: "9", 10: "10", 100: "100", 1000: "1000"}
	for n, want := range cases {
		if got := Itoa(n); got != want {
			t.Errorf("Itoa(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestRendererIncremental(t *testing.T) {
	r := New()
	f := NewFrame(8, 2)
	f.Cells[0][0] = emulator.Cell{Content: "a", Width: 1}
	f.Cells[1][0] = emulator.Cell{Content: "b", Width: 1}

	var sb strings.Builder
	r.Draw(&sb, f)

	sb.Reset()
	f.Cells[1][4] = emulator.Cell{Content: "x", Width: 1}
	r.Draw(&sb, f)
	out := sb.String()

	if !strings.Contains(out, "\x1b[2;5H") {
		t.Errorf("changed row must be positioned at its first changed cell: %q", out)
	}
	if !strings.Contains(out, "x") {
		t.Errorf("changed content missing: %q", out)
	}
	if strings.Contains(out, "a") {
		t.Errorf("unchanged row must not be re-emitted: %q", out)
	}
}

func TestRendererSkipsIdenticalFrame(t *testing.T) {
	r := New()
	f := NewFrame(8, 2)
	f.Cells[0][0] = emulator.Cell{Content: "a", Width: 1}
	f.Cells[1][0] = emulator.Cell{Content: "b", Width: 1}

	var sb strings.Builder
	r.Draw(&sb, f)

	sb.Reset()
	r.Draw(&sb, f)
	out := sb.String()

	if strings.Contains(out, "a") || strings.Contains(out, "b") {
		t.Errorf("identical frame must not re-emit rows: %q", out)
	}
	if !strings.Contains(out, "\x1b[?25l") {
		t.Error("cursor must still be hidden")
	}
	if !strings.Contains(out, "\x1b[?25h") {
		t.Error("cursor must still be shown")
	}
}

func TestRendererErasesRemovedTail(t *testing.T) {
	r := New()
	f := NewFrame(8, 1)
	for x, ch := range []string{"l", "o", "n", "g", "t", "e", "x", "t"} {
		f.Cells[0][x] = emulator.Cell{Content: ch, Width: 1}
	}

	var sb strings.Builder
	r.Draw(&sb, f)

	sb.Reset()
	// Blank replacement row carries terminal-default colors like any frame.
	row := make([]emulator.Cell, 8)
	def := emulator.Color{Default: true}
	for i := range row {
		row[i].Fg, row[i].Bg = def, def
	}
	f.Cells[0] = row
	f.Cells[0][0] = emulator.Cell{Content: "x", Width: 1}
	r.Draw(&sb, f)
	out := sb.String()

	if !strings.Contains(out, "\x1b[K") {
		t.Errorf("removed content must be erased: %q", out)
	}
}

func TestRendererFullRedrawOnResize(t *testing.T) {
	r := New()
	f := NewFrame(8, 1)
	f.Cells[0][0] = emulator.Cell{Content: "a", Width: 1}

	var sb strings.Builder
	r.Draw(&sb, f)

	f = NewFrame(8, 2)
	f.Cells[0][0] = emulator.Cell{Content: "a", Width: 1}
	f.Cells[1][0] = emulator.Cell{Content: "b", Width: 1}
	sb.Reset()
	r.Draw(&sb, f)
	out := sb.String()

	if !strings.Contains(out, "\x1b[2;1H") {
		t.Errorf("new row must be drawn after a resize: %q", out)
	}
	if !strings.Contains(out, "b") {
		t.Errorf("new row content missing: %q", out)
	}
}

// A change landing first on a wide character's continuation cell used to
// start the redraw mid-glyph: the placeholder itself is skipped (so a
// cursor/selection change on the right half emitted nothing at all), and
// any later writes landed one column short. The diff must back up to the
// lead cell and re-emit the glyph.
func TestRendererDiffOnContinuationCell(t *testing.T) {
	r := New()
	f := NewFrame(8, 1)
	f.Cells[0][0] = emulator.Cell{Content: "中", Width: 2}
	f.Cells[0][1] = emulator.Cell{Width: 0}

	var sb strings.Builder
	r.Draw(&sb, f)

	sb.Reset()
	// Only the continuation half changes.
	f.Cells[0][1].Reverse = true
	r.Draw(&sb, f)
	out := sb.String()

	if !strings.Contains(out, "\x1b[1;1H") {
		t.Fatalf("redraw must start at the lead cell (column 1): %q", out)
	}
	if !strings.Contains(out, "中") {
		t.Fatalf("lead glyph must be re-emitted so the row stays aligned: %q", out)
	}
}
