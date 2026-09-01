package app

import (
	"strings"
	"testing"

	"vimterm/internal/keybind"
	"vimterm/internal/mode"
)

// TestBackspaceInInsertModeBytes guards against the ConPTY quirk where a BS
// byte (0x08) is delivered to the child as Ctrl+Backspace (delete word):
// backspace must be forwarded as DEL (0x7F), which deletes one character.
func TestBackspaceInInsertModeBytes(t *testing.T) {
	a := newMotionApp(t, 40, 5, "abc\r\n")
	fs := &fakeSession{}
	a.sess = fs

	press(t, a, keybind.NewRune('i', 0))
	for _, r := range "hello" {
		press(t, a, keybind.NewRune(r, 0))
	}
	if got := string(fs.writes); got != "hello" {
		t.Fatalf("typed bytes = %q, want hello", got)
	}
	press(t, a, keybind.NewCode(keybind.CodeBackspace, 0))
	want := "hello" + string([]byte{0x7F})
	if got := string(fs.writes); got != want {
		t.Fatalf("after backspace bytes = %q, want %q", got, want)
	}
}

// TestMoveShellCursorSkipsPreviousOutput verifies that pressing i on a
// blank output line (above the prompt) does NOT send cross-line escape
// sequences to the shell. The virtual cursor snaps to the shell position.
func TestMoveShellCursorSkipsPreviousOutput(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("\r\n")
	sb.WriteString("prompt> hi\r\n")
	a := newMotionApp(t, 40, 5, sb.String())
	fs := &fakeSession{}
	a.sess = fs

	cx, cy := a.emu.Cursor()
	scrollbackLen := a.emu.ScrollbackLen()
	shellAbs := scrollbackLen + cy
	t.Logf("emu cursor: (%d, %d), shellAbs=%d", cx, cy, shellAbs)

	// Navigate to line 0 (previous output, non-full-width row).
	a.cur.Line = 0
	a.cur.Col = 5
	a.curValid = true

	fs.writes = nil
	a.moveShellCursorToVirtual()

	if len(fs.writes) > 0 {
		t.Fatalf("moveShellCursorToVirtual on output line sent %d bytes %q, want 0 bytes",
			len(fs.writes), string(fs.writes))
	}
	if a.cur.Line != shellAbs {
		t.Fatalf("cur.Line = %d after snap, want %d (shell position)", a.cur.Line, shellAbs)
	}
}

// TestMoveShellCursorKeepsPositionOnWrappedCommand verifies that pressing i
// on the first line of a wrapped command does NOT snap to the end. The
// first line of a wrapped command is full-width, so it is recognised as
// part of the command.
func TestMoveShellCursorKeepsPositionOnWrappedCommand(t *testing.T) {
	// 40-col terminal.
	// line 0: output (shorter than width)
	// line 1: prompt + command fills entire width (40 chars)
	// line 2: continuation (cursor here, 1 char → last cell empty)
	var sb strings.Builder
	sb.WriteString("output above\r\n")
	sb.WriteString("prompt> ")
	for i := 0; i < 40; i++ {
		sb.WriteByte('x')
	}
	sb.WriteString("y")
	a := newMotionApp(t, 40, 5, sb.String())
	fs := &fakeSession{}
	a.sess = fs

	scrollbackLen := a.emu.ScrollbackLen()
	_, cy := a.emu.Cursor()
	shellAbs := scrollbackLen + cy
	t.Logf("shellAbs=%d", shellAbs)

	// Navigate to line 1 (first line of wrapped command, full-width row).
	a.cur.Line = 1
	a.cur.Col = 10
	a.curValid = true

	fs.writes = nil
	a.moveShellCursorToVirtual()

	// Should NOT snap — line 1 is full-width (part of the command).
	if a.cur.Line != 1 {
		t.Fatalf("cur.Line = %d, want 1 (must stay on command line)", a.cur.Line)
	}
	// Cross-line conversion should fire (rowDelta = 1 - 2 = -1 → right arrows).
	if len(fs.writes) == 0 {
		t.Fatal("moveShellCursorToVirtual sent 0 bytes, expected cross-line escape sequences")
	}
}

// TestJThenIOnWrappedCommand verifies that pressing j to move from the upper
// line to the lower line of a wrapped command, then i to enter insert mode,
// keeps the cursor on the lower line (the shell cursor row).
func TestJThenIOnWrappedCommand(t *testing.T) {
	// 40-col terminal.
	// line 0: output (short, not full-width)
	// line 1: prompt + command fills entire width (40 chars, full-width row)
	// line 2: continuation (1 char → not full-width, shell cursor row)
	var sb strings.Builder
	sb.WriteString("output above\r\n")
	sb.WriteString("prompt> ")
	for i := 0; i < 40; i++ {
		sb.WriteByte('x')
	}
	sb.WriteString("y")
	a := newMotionApp(t, 40, 5, sb.String())
	fs := &fakeSession{}
	a.sess = fs

	scrollbackLen := a.emu.ScrollbackLen()
	_, cy := a.emu.Cursor()
	shellAbs := scrollbackLen + cy
	t.Logf("shellAbs=%d", shellAbs)

	// Start on line 1 (upper line of wrapped command, full-width row).
	a.cur.Line = 1
	a.cur.Col = 10
	a.curValid = true

	// Press j to move to line 2 (lower line = shell cursor row).
	press(t, a, keybind.NewRune('j', 0))

	if a.cur.Line != shellAbs {
		t.Fatalf("after j: cur.Line = %d, want %d (shell cursor row)", a.cur.Line, shellAbs)
	}

	// Press i to enter insert mode.
	press(t, a, keybind.NewRune('i', 0))

	if a.mods.Current() != mode.ModeInsert {
		t.Fatal("not in insert mode after pressing i")
	}
	if a.cur.Line != shellAbs {
		t.Fatalf("after i: cur.Line = %d, want %d (must stay on shell cursor row)", a.cur.Line, shellAbs)
	}
}

// TestIOnUpperLineOfWrappedCommand verifies that pressing i on the upper
// line of a wrapped command moves the shell cursor to the upper line via
// cross-line escape sequences, and the cursor stays there.
func TestIOnUpperLineOfWrappedCommand(t *testing.T) {
	// 40-col terminal.
	// line 0: output (short, not full-width)
	// line 1: prompt + command fills entire width (40 chars, full-width row)
	// line 2: continuation (1 char → not full-width, shell cursor row)
	var sb strings.Builder
	sb.WriteString("output above\r\n")
	sb.WriteString("prompt> ")
	for i := 0; i < 40; i++ {
		sb.WriteByte('x')
	}
	sb.WriteString("y")
	a := newMotionApp(t, 40, 5, sb.String())
	fs := &fakeSession{}
	a.sess = fs

	scrollbackLen := a.emu.ScrollbackLen()
	_, cy := a.emu.Cursor()
	shellAbs := scrollbackLen + cy
	t.Logf("shellAbs=%d", shellAbs)

	// Position on line 1 (upper line of wrapped command, full-width row).
	a.cur.Line = 1
	a.cur.Col = 10
	a.curValid = true

	fs.writes = nil
	press(t, a, keybind.NewRune('i', 0))

	if a.mods.Current() != mode.ModeInsert {
		t.Fatal("not in insert mode after pressing i")
	}
	// Cursor must stay on line 1 — not snap to shellAbs.
	if a.cur.Line != 1 {
		t.Fatalf("after i: cur.Line = %d, want 1 (must stay on upper command line)", a.cur.Line)
	}
	// Cross-line conversion should fire (rowDelta = 1 - shellAbs = -1 → left arrows).
	if len(fs.writes) == 0 {
		t.Fatal("moveShellCursorToVirtual sent 0 bytes, expected cross-line escape sequences")
	}
}

// TestMoveShellCursorOnNonFullWidthCommandLine verifies that the snap does NOT
// fire when the cursor is on a non-full-width line that IS part of the wrapped
// command. This happens when the shell cursor has been moved (e.g. user pressed
// Home or left arrows) so that shellAbs is on an earlier line. The last line of
// the wrapped command is not full-width but is still part of the command.
func TestMoveShellCursorOnNonFullWidthCommandLine(t *testing.T) {
	// 40-col terminal, 5-row host (4 visible rows).
	// line 0: output (short)
	// line 1: prompt + command fills entire width (40 chars, full-width)
	// line 2: continuation "y" (1 char, not full-width) — original shell cursor row
	var sb strings.Builder
	sb.WriteString("output above\r\n")
	sb.WriteString("prompt> ")
	for i := 0; i < 40; i++ {
		sb.WriteByte('x')
	}
	sb.WriteString("y")
	a := newMotionApp(t, 40, 5, sb.String())
	fs := &fakeSession{}
	a.sess = fs

	scrollbackLen := a.emu.ScrollbackLen()
	cx, cy := a.emu.Cursor()
	origShellAbs := scrollbackLen + cy
	t.Logf("original shellAbs=%d, cursor=(%d,%d)", origShellAbs, cx, cy)

	// Move the shell cursor back to line 1 (the full-width line) using
	// absolute cursor positioning: ESC[2;1H = row 2, col 1 (1-indexed).
	if _, err := a.emu.Write([]byte("\x1b[2;1H")); err != nil {
		t.Fatal(err)
	}

	// After moving, the shell cursor should be on line 1.
	cx, cy = a.emu.Cursor()
	shellAbs := scrollbackLen + cy
	t.Logf("after move: emu cursor (%d, %d), shellAbs=%d", cx, cy, shellAbs)
	if shellAbs >= origShellAbs {
		t.Fatalf("shell cursor did not move up: shellAbs=%d, origShellAbs=%d", shellAbs, origShellAbs)
	}

	// Navigate the virtual cursor to line 2 (last line of wrapped command,
	// NOT full-width, NOT the shell cursor row).
	a.cur.Line = scrollbackLen + 2
	a.cur.Col = 0
	a.curValid = true

	fs.writes = nil
	a.moveShellCursorToVirtual()

	// The snap must NOT fire — line 2 is part of the wrapped command.
	if a.cur.Line != scrollbackLen+2 {
		t.Fatalf("snap fired: cur.Line = %d, want %d (line 2 must stay)",
			a.cur.Line, scrollbackLen+2)
	}
	// Cross-line conversion should move shell cursor down one visual line.
	if len(fs.writes) == 0 {
		t.Fatal("moveShellCursorToVirtual sent 0 bytes, expected cross-line escape sequences")
	}
}

// TestJThenIMustStayOnLastLine verifies the user scenario: user is on the
// upper (full-width) line of a wrapped command, presses j to move to the
// last line (not full-width), then i to enter insert mode. The cursor must
// stay on the last line, not snap back to the upper line.
func TestJThenIMustStayOnLastLine(t *testing.T) {
	// 40-col terminal.
	// line 0: prompt + command fills entire width (40 chars, full-width)
	// line 1: continuation "y" (1 char, not full-width, shell cursor row)
	var sb strings.Builder
	sb.WriteString("prompt> ")
	for i := 0; i < 40; i++ {
		sb.WriteByte('x')
	}
	sb.WriteString("y")
	a := newMotionApp(t, 40, 5, sb.String())
	fs := &fakeSession{}
	a.sess = fs

	scrollbackLen := a.emu.ScrollbackLen()
	_, cy := a.emu.Cursor()
	origShellAbs := scrollbackLen + cy
	t.Logf("shellAbs=%d", origShellAbs)

	// Move the shell cursor back to line 0 (the full-width line) using
	// absolute cursor positioning: ESC[1;1H = row 1, col 1 (1-indexed).
	if _, err := a.emu.Write([]byte("\x1b[1;1H")); err != nil {
		t.Fatal(err)
	}

	_, cy = a.emu.Cursor()
	shellAbs := scrollbackLen + cy
	t.Logf("after move: shellAbs=%d", shellAbs)

	// Start on the upper (full-width) line.
	a.cur.Line = scrollbackLen
	a.cur.Col = 10
	a.curValid = true

	// Press j to go to the last line (not full-width, not shell cursor row).
	press(t, a, keybind.NewRune('j', 0))
	t.Logf("after j: cur.Line=%d", a.cur.Line)

	// Press i to enter insert mode.
	press(t, a, keybind.NewRune('i', 0))

	if a.mods.Current() != mode.ModeInsert {
		t.Fatal("not in insert mode after pressing i")
	}
	// The cursor must stay on the last line — it must NOT snap back to
	// the upper line where the shell cursor is.
	if a.cur.Line != scrollbackLen+1 {
		t.Fatalf("after i: cur.Line = %d, want %d (must stay on last command line, not snap to shellAbs=%d)",
			a.cur.Line, scrollbackLen+1, shellAbs)
	}
}

// TestMoveShellCursorBelowShellAbsOnNonFullWidth verifies that the snap does
// NOT fire when the cursor is below the shell cursor row on a non-full-width
// line. The cross-line conversion should apply instead.
func TestMoveShellCursorBelowShellAbsOnNonFullWidth(t *testing.T) {
	// 40-col terminal.
	// line 0: prompt + command (not full-width, shell cursor row)
	// line 1: empty (not full-width)
	var sb strings.Builder
	sb.WriteString("prompt> hi\r\n")
	a := newMotionApp(t, 40, 5, sb.String())
	fs := &fakeSession{}
	a.sess = fs

	scrollbackLen := a.emu.ScrollbackLen()
	_, cy := a.emu.Cursor()
	shellAbs := scrollbackLen + cy
	t.Logf("shellAbs=%d", shellAbs)

	// Navigate to line below shell cursor (empty line).
	a.cur.Line = shellAbs + 1
	a.cur.Col = 0
	a.curValid = true

	fs.writes = nil
	a.moveShellCursorToVirtual()

	// Snap should NOT fire — lines below the shell cursor are never
	// snapped. The cross-line conversion applies instead.
	if a.cur.Line != shellAbs+1 {
		t.Fatalf("snap fired: cur.Line = %d, want %d (must stay below shellAbs)",
			a.cur.Line, shellAbs+1)
	}
}
