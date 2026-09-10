package emulator

import (
	"testing"

	"github.com/charmbracelet/x/vt"
)

func TestMouseModeCountDrift(t *testing.T) {
	// Access the concrete type to reach mouseModeCount for verification.
	e := New(80, 24)
	var cb vt.Callbacks
	e.SetCallbacks(cb)

	// Enable mouse mode (DECSET 1003).
	e.Write([]byte("\x1b[?1003h"))
	if !e.IsMouseTracking() {
		t.Fatal("expected tracking after first enable")
	}

	// Send the same DECSET again (idempotent in real terminals).
	// The vt library calls EnableMode for every DECSET, even if already set.
	e.Write([]byte("\x1b[?1003h"))
	if !e.IsMouseTracking() {
		t.Fatal("expected tracking after second enable")
	}

	// Disable once. Real terminals: tracking is now off.
	// Bug: count becomes 2-1=1, so IsMouseTracking still returns true,
	// but the vt library's internal mode state is "disabled".
	e.Write([]byte("\x1b[?1003l"))
	if e.IsMouseTracking() {
		t.Fatal("tracking should be off after single disable, but IsMouseTracking returned true (mouseModeCount drift)")
	}
}

func TestMouseModeCountNoUnderflow(t *testing.T) {
	e := New(80, 24)
	var cb vt.Callbacks
	e.SetCallbacks(cb)

	// Disable without prior enable: count should stay at 0.
	e.Write([]byte("\x1b[?1003l"))
	if e.IsMouseTracking() {
		t.Fatal("should not be tracking without any enable")
	}
}

func TestReadCellsPanicsOnSmallDst(t *testing.T) {
	e := New(10, 10)
	// Undersized destination: needs 10*10=100, only has 5.
	dst := make([]Cell, 5)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on undersized dst, but no panic occurred")
		}
	}()
	e.ReadCells(0, 0, 10, 10, dst)
}

func TestReadScrollbackCellsPanicsOnSmallDst(t *testing.T) {
	e := New(10, 2)
	// Push line into scrollback.
	e.Write([]byte("hello\r\n"))
	e.Write([]byte("world\r\n"))
	if e.ScrollbackLen() == 0 {
		t.Fatal("expected scrollback")
	}
	// Undersized destination: needs 10, only has 3.
	dst := make([]Cell, 3)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on undersized scrollback dst, but no panic occurred")
		}
	}()
	e.ReadScrollbackCells(0, 0, 10, dst)
}

func TestDeleteLineCellsOnScrollback(t *testing.T) {
	e := New(10, 2)
	// Push "line1" into scrollback.
	e.Write([]byte("line1\r\n"))
	e.Write([]byte("line2\r\n"))
	e.Write([]byte("line3\r\n"))
	sbLen := e.ScrollbackLen()
	if sbLen == 0 {
		t.Fatal("expected scrollback")
	}

	// Delete 2 cells at column 0 of the oldest scrollback line.
	e.DeleteLineCells(0, 0, 2)
	got := e.ScrollbackCell(0, 0).Content
	if got != "n" {
		t.Fatalf("after delete: cell(0,0) = %q, want %q", got, "n")
	}
}

func TestDeleteLineCellsClamps(t *testing.T) {
	e := New(10, 2)
	// Use 2 rows so \r\n doesn't scroll the content off.
	e.Write([]byte("abc\r\n"))
	// Delete past end of line: n should be clamped so we don't shift garbage.
	// col=8 is valid (0-9), n=5 clamps to n=2 (cols 8-9).
	// Shift copies from col 10 (empty), so only the tail gets blanked.
	e.DeleteLineCells(0, 8, 5)
	// Columns 0-7 unchanged, columns 8-9 blanked.
	got := e.Cell(0, 0).Content
	if got != "a" {
		t.Fatalf("cell(0,0) = %q, want a (only tail should be blanked)", got)
	}
}

func TestDeleteLineCellsOutOfBoundsColIsNoop(t *testing.T) {
	e := New(10, 2)
	e.Write([]byte("abc\r\n"))
	// col=10 equals width, which is >= cols → early return (noop).
	e.DeleteLineCells(0, 10, 5)
	got := e.Cell(0, 0).Content
	if got != "a" {
		t.Fatalf("cell(0,0) = %q, want a (out-of-bounds col is noop)", got)
	}
}
