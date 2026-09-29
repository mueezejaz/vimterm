package app

import (
	"testing"
	"time"

	"vimterm/internal/keybind"
	"vimterm/internal/mode"
)

func TestTextEnd(t *testing.T) {
	if got := textEnd([]rune("abc   ")); got != 2 {
		t.Errorf("textEnd(abc) = %d, want 2", got)
	}
	if got := textEnd([]rune("     ")); got != -1 {
		t.Errorf("textEnd(empty) = %d, want -1", got)
	}
}

func TestInsertAfter(t *testing.T) {
	a := findApp(t, "abc\r\n")
	press(t, a, keybind.NewRune('l', 0))
	press(t, a, keybind.NewRune('a', 0))
	if a.mods.Current() != mode.ModeInsert {
		t.Fatal("a must enter insert mode")
	}
	if a.cur.Col != 2 {
		t.Fatalf("a after l: col = %d, want 2", a.cur.Col)
	}
}

func TestInsertEnd(t *testing.T) {
	a := findApp(t, "abc\r\n")
	press(t, a, keybind.NewRune('A', keybind.ModShift))
	if a.mods.Current() != mode.ModeInsert {
		t.Fatal("A must enter insert mode")
	}
	if a.cur.Col != 3 {
		t.Fatalf("A: col = %d, want 3", a.cur.Col)
	}
}

func TestInsertEndOnSpaceOnlyLine(t *testing.T) {
	a := findApp(t, "   \r\n")
	press(t, a, keybind.NewRune('A', keybind.ModShift))
	if a.mods.Current() != mode.ModeInsert {
		t.Fatal("A must enter insert mode")
	}
	// On an all-space line, textEnd returns -1 and the cursor goes to col 0,
	// matching Vim's behavior where A and i are identical on blank lines.
	if a.cur.Col != 0 {
		t.Fatalf("A on spaces: col = %d, want 0 (same as i on blank line)", a.cur.Col)
	}
}

func TestInsertHome(t *testing.T) {
	a := findApp(t, "  abc\r\n")
	press(t, a, keybind.NewRune('I', keybind.ModShift))
	if a.mods.Current() != mode.ModeInsert {
		t.Fatal("I must enter insert mode")
	}
	if a.cur.Col != 2 {
		t.Fatalf("I: col = %d, want 2", a.cur.Col)
	}
}

// Re-entering insert mode mid-command must not snap the cursor back to the
// previous insert column and then forward again. i hands the shell cursor a
// relative move, but the arrow keys reach the shell asynchronously: the
// emulator keeps reporting the old column until the echo comes back, a frame
// or more later. Rendering that stale column is what makes the cursor
// flicker, so the frames drawn in between must show the insert point.
func TestReenterInsertNoRoundTripFlicker(t *testing.T) {
	a := findApp(t, "echo hello")
	a.sess = &fakeSession{}
	a.mods.Enter(mode.ModeInsert)
	a.curValid = false

	// Typing left the shell cursor at the end of the command.
	if cx, _ := a.shellCursorPos(); cx != 10 {
		t.Fatalf("precondition: shell cursor = %d, want 10", cx)
	}

	// Esc, then four h's put the virtual cursor mid-command.
	press(t, a, keybind.NewCode(keybind.CodeEsc, 0))
	for i := 0; i < 4; i++ {
		press(t, a, keybind.NewRune('h', 0))
	}
	if a.cur.Col != 6 {
		t.Fatalf("virtual cursor after 4h: col = %d, want 6", a.cur.Col)
	}

	// i commands the shell cursor back to col 6; the emulator still reports
	// the stale col 10 until the shell echoes.
	press(t, a, keybind.NewRune('i', 0))
	if !a.mods.Is(mode.ModeInsert) {
		t.Fatal("i must re-enter insert mode")
	}
	if cx, _ := a.emu.Cursor(); cx != 10 {
		t.Fatalf("precondition: emulator cursor = %d, want the stale 10", cx)
	}
	if cx, _ := a.shellCursorPos(); cx != 6 {
		t.Fatalf("shell cursor before the echo = %d, want the insert point 6", cx)
	}

	// The shell echo lands on the insert point: nothing moves, so the frame
	// after the echo must report the same column.
	if _, err := a.emu.Write([]byte("\x1b[4D")); err != nil {
		t.Fatal(err)
	}
	if cx, _ := a.emu.Cursor(); cx != 6 {
		t.Fatalf("shell cursor after echo = %d, want 6", cx)
	}
	if cx, _ := a.shellCursorPos(); cx != 6 {
		t.Fatalf("shell cursor after the echo = %d, want 6", cx)
	}
}

// The prediction must not outlive the echo: a shell that ignores the arrow
// keys (or a command that is not a line editor) has to fall back to the real
// cursor rather than showing a guessed position indefinitely.
func TestShellCursorPredictionExpires(t *testing.T) {
	a := findApp(t, "echo hello")
	a.sess = &fakeSession{}
	a.mods.Enter(mode.ModeInsert)
	a.curValid = false

	press(t, a, keybind.NewCode(keybind.CodeEsc, 0))
	for i := 0; i < 4; i++ {
		press(t, a, keybind.NewRune('h', 0))
	}
	press(t, a, keybind.NewRune('i', 0))

	// No echo ever arrives.
	if cx, _ := a.shellCursorPos(); cx != 6 {
		t.Fatalf("before expiry = %d, want the prediction 6", cx)
	}
	a.pendingUntil = time.Now().Add(-time.Millisecond)
	if cx, _ := a.shellCursorPos(); cx != 10 {
		t.Fatalf("after expiry = %d, want the emulator's 10", cx)
	}
	if a.pendingValid {
		t.Fatal("expired prediction should be cleared")
	}
}

// A prediction is dropped as soon as the emulator agrees, so a shell that
// puts the cursor exactly where it was asked leaves no stale state behind.
func TestShellCursorPredictionClearedOnEcho(t *testing.T) {
	a := findApp(t, "echo hello")
	a.sess = &fakeSession{}
	a.mods.Enter(mode.ModeInsert)
	a.curValid = false

	press(t, a, keybind.NewCode(keybind.CodeEsc, 0))
	for i := 0; i < 4; i++ {
		press(t, a, keybind.NewRune('h', 0))
	}
	press(t, a, keybind.NewRune('i', 0))
	if !a.pendingValid {
		t.Fatal("a cursor move should arm the prediction")
	}

	if _, err := a.emu.Write([]byte("\x1b[4D")); err != nil {
		t.Fatal(err)
	}
	if _, _ = a.shellCursorPos(); a.pendingValid {
		t.Fatal("prediction should clear once the emulator agrees")
	}
}

func TestInsertAfterMovesRightOnce(t *testing.T) {
	a := findApp(t, "abc\r\n")
	press(t, a, keybind.NewRune('l', 0))
	press(t, a, keybind.NewRune('a', 0))
	if a.mods.Current() != mode.ModeInsert {
		t.Fatal("a must enter insert mode")
	}
	if a.cur.Col != 2 {
		t.Fatalf("l a: col = %d, want 2", a.cur.Col)
	}
}
