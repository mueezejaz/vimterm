package app

import (
	"strings"
	"testing"
	"time"

	"vimterm/internal/keybind"
	"vimterm/internal/pty"
)

// TestDeletePropagatesToRealShell spawns a real PowerShell, types a command
// line, then deletes the last word with dw and types in insert mode. The
// shell's line editor must not restore the deleted word on redraw.
func TestDeletePropagatesToRealShell(t *testing.T) {
	const cols, rows = 80, 24
	sess, err := pty.Spawn("powershell.exe", nil, cols, rows)
	if err != nil {
		t.Skipf("cannot spawn shell: %v", err)
	}
	defer func() {
		_ = sess.Kill()
		_ = sess.Close()
	}()

	a := newMotionApp(t, cols, rows, "")
	a.sess = sess
	a.clipWrite = func(s string) error { return nil }

	emu := a.emu
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				_, _ = emu.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	deadline := time.Now().Add(20 * time.Second)
	waitFor := func(needle string) bool {
		for time.Now().Before(deadline) {
			if strings.Contains(gridText(emu, cols, rows), needle) {
				return true
			}
			time.Sleep(100 * time.Millisecond)
		}
		return false
	}
	if !waitFor(">") {
		t.Fatalf("prompt not seen; grid:\n%s", gridText(emu, cols, rows))
	}
	time.Sleep(500 * time.Millisecond)

	if _, err := sess.Write([]byte("echo hello world")); err != nil {
		t.Fatalf("write command: %v", err)
	}
	if !waitFor("echo hello world") {
		t.Fatalf("command echo not seen; grid:\n%s", gridText(emu, cols, rows))
	}
	time.Sleep(300 * time.Millisecond)

	// b moves to the start of the last word, dw deletes it.
	press(t, a, keybind.NewRune('b', 0))
	press(t, a, keybind.NewRune('d', 0))
	press(t, a, keybind.NewRune('w', 0))
	if !waitFor("echo hello") {
		t.Fatalf("after dw grid missing 'echo hello'; grid:\n%s", gridText(emu, cols, rows))
	}
	if strings.Contains(gridText(emu, cols, rows), "world") {
		t.Fatalf("after dw 'world' still present; grid:\n%s", gridText(emu, cols, rows))
	}

	// Enter insert mode and type a character: the shell redraws its line.
	press(t, a, keybind.NewRune('i', 0))
	press(t, a, keybind.NewRune('X', 0))
	if !waitFor("X") {
		t.Fatalf("typed char not seen; grid:\n%s", gridText(emu, cols, rows))
	}
	time.Sleep(300 * time.Millisecond)
	if strings.Contains(gridText(emu, cols, rows), "world") {
		t.Fatalf("'world' came back after typing in insert mode; grid:\n%s",
			gridText(emu, cols, rows))
	}
	if !strings.Contains(gridText(emu, cols, rows), "echo hello X") {
		t.Fatalf("expected 'echo hello X'; grid:\n%s", gridText(emu, cols, rows))
	}
}