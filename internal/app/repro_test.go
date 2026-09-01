package app

import (
	"testing"

	"vimterm/internal/keybind"
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
