package app

import (
	"strings"
	"testing"
	"time"

	"vimterm/internal/keybind"
	"vimterm/internal/pty"
)

// TestBackspaceEndToEnd drives keys through the app's insert-mode passthrough
// into a real cmd.exe session over ConPTY and verifies one backspace deletes
// exactly one character on the shell's line editor.
func TestBackspaceEndToEnd(t *testing.T) {
	const cols, rows = 120, 15
	sess, err := pty.Spawn("cmd.exe", nil, cols, rows)
	if err != nil {
		t.Skipf("cannot spawn cmd: %v", err)
	}
	defer func() {
		_ = sess.Kill()
		_ = sess.Close()
	}()

	a := findApp(t, "\r\n")
	a.sess = sess
	emu := a.emu
	grid := func() string {
		var sb strings.Builder
		for y := 0; y < a.emu.Height(); y++ {
			for x := 0; x < a.emu.Width(); x++ {
				c := a.emu.Cell(x, y)
				if c.Content == "" {
					sb.WriteByte(' ')
				} else {
					sb.WriteString(c.Content)
				}
			}
			sb.WriteByte('\n')
		}
		return sb.String()
	}

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

	time.Sleep(2 * time.Second)
	press(t, a, keybind.NewRune('i', 0))
	for _, r := range "Get-ChildItem" {
		press(t, a, keybind.NewRune(r, 0))
	}
	waitFor := func(sub string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(grid(), sub) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("never saw %q:\n%s", sub, grid())
	}
	waitFor("Get-ChildItem")

	press(t, a, keybind.NewCode(keybind.CodeBackspace, 0))
	waitFor("Get-ChildIte")

	press(t, a, keybind.NewCode(keybind.CodeBackspace, 0))
	waitFor("Get-ChildIt")
}