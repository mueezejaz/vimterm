package emulator

import (
	"io"
	"testing"
	"time"

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

// TestRepliesUnblocksWriteOnCPR is the regression test for the terminal
// freeze: a child asking "where is the cursor?" (CSI 6n) used to wedge Write
// forever because nothing drained the reply pipe.
func TestRepliesUnblocksWriteOnCPR(t *testing.T) {
	e := New(80, 24)
	defer e.Close()

	replies := make(chan string, 4)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := e.Replies().Read(buf)
			if n > 0 {
				replies <- string(buf[:n])
			}
			if err != nil {
				close(replies)
				return
			}
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = e.Write([]byte("hello\x1b[6n"))
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Write blocked on CSI 6n: the reply pipe was never drained")
	}

	select {
	case got := <-replies:
		// "hello" leaves the cursor on row 1, column 6.
		if want := "\x1b[1;6R"; got != want {
			t.Errorf("CPR reply = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no CPR reply was produced")
	}
}

func TestRepliesEOFAfterClose(t *testing.T) {
	e := New(80, 24)
	r := e.Replies()
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	buf := make([]byte, 64)
	if _, err := r.Read(buf); err != io.EOF {
		t.Fatalf("Read after Close = %v, want io.EOF", err)
	}
}

// TestRepliesStreamsDeviceAttributes covers the other reply families: DA1
// and in-band resize, both of which a real child (fzf, tmux) depends on.
func TestRepliesStreamsDeviceAttributes(t *testing.T) {
	e := New(80, 24)
	defer e.Close()

	got := readReply(t, e, "\x1b[c")
	if got == "" {
		t.Fatal("DA1 produced no reply")
	}
	if got[0] != 0x1b || got[1] != '[' || got[2] != '?' {
		t.Errorf("DA1 reply = %q, want a CSI ? ... c report", got)
	}
}

func TestRepliesInBandResize(t *testing.T) {
	e := New(80, 24)
	defer e.Close()

	got := readReply(t, e, "\x1b[?2048h\x1b[8;50;100t")
	if got == "" {
		t.Fatal("in-band resize produced no reply")
	}
	if got[0] != 0x1b || got[1] != '[' {
		t.Errorf("in-band resize reply = %q, want a CSI report", got)
	}
}

// readReply feeds seq to the emulator with a reply reader running and returns
// the first reply produced.
func readReply(t *testing.T, e Emulator, seq string) string {
	t.Helper()
	ch := make(chan string, 4)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := e.Replies().Read(buf)
			if n > 0 {
				ch <- string(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = e.Write([]byte(seq))
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Write blocked: reply pipe was never drained")
	}

	select {
	case s := <-ch:
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a reply")
		return ""
	}
}
