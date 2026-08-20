package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vimterm/internal/keybind"
)

// fakeSession records writes for headless paste tests.
type fakeSession struct {
	writes []byte
}

func (f *fakeSession) Write(p []byte) (int, error) {
	f.writes = append(f.writes, p...)
	return len(p), nil
}

func (f *fakeSession) Read(p []byte) (int, error) { return 0, nil }
func (f *fakeSession) Resize(int, int) error      { return nil }
func (f *fakeSession) Kill() error                { return nil }
func (f *fakeSession) Close() error               { return nil }
func (f *fakeSession) Name() string               { return "fake" }
func (f *fakeSession) Wait(ctx context.Context) error { return nil }

func pasteApp(t *testing.T, text string) (*App, *fakeSession) {
	t.Helper()
	// No trailing CRLF: the emulator cursor then sits on line 0 like a real
	// shell prompt, so the paste nudge (arrow keys) applies.
	a := findApp(t, "abc")
	fs := &fakeSession{}
	a.sess = fs
	a.clipRead = func() (string, error) { return text, nil }
	press(t, a, keybind.NewRune('g', 0))
	press(t, a, keybind.NewRune('g', 0))
	return a, fs
}

func TestPasteWritesClipboard(t *testing.T) {
	a, fs := pasteApp(t, "hello")
	press(t, a, keybind.NewRune('p', 0))
	// p moves one right of the virtual cursor and nudges the shell cursor
	// there: cursor at 3, virtual at 1 -> two left arrows, then the text.
	want := "\x1b[D\x1b[D" + "hello"
	if string(fs.writes) != want {
		t.Fatalf("p wrote %q, want %q", fs.writes, want)
	}
}

func TestPasteAfterCursorNudgesFirst(t *testing.T) {
	a, fs := pasteApp(t, "x")
	press(t, a, keybind.NewRune('l', 0))
	press(t, a, keybind.NewRune('p', 0))
	// One left arrow moves the shell cursor to the virtual position, then
	// the text itself.
	want := "\x1b[D" + "x"
	if string(fs.writes) != want {
		t.Fatalf("p wrote %q, want %q", fs.writes, want)
	}
}

func TestPasteBeforeNoNudge(t *testing.T) {
	a, fs := pasteApp(t, "x")
	press(t, a, keybind.NewRune('P', keybind.ModShift))
	if string(fs.writes) != "\x1b[D\x1b[D\x1b[Dx" {
		t.Fatalf("P wrote %q, want 3 left arrows + x", fs.writes)
	}
}

func TestPasteWithCount(t *testing.T) {
	a, fs := pasteApp(t, "ab")
	pressDigits(t, a, "3")
	press(t, a, keybind.NewRune('p', 0))
	if string(fs.writes) != "\x1b[D\x1b[Dababab" {
		t.Fatalf("3p wrote %q, want 2 left arrows + ababab", fs.writes)
	}
}

func TestPasteEmptyClipboard(t *testing.T) {
	a, fs := pasteApp(t, "")
	press(t, a, keybind.NewRune('p', 0))
	if len(fs.writes) != 0 {
		t.Fatalf("p with empty clipboard wrote %q", fs.writes)
	}
	if msg := a.statusMsg(); msg == "" {
		t.Fatal("expected an empty-clipboard status message")
	}
}

func TestPasteClipboardError(t *testing.T) {
	a, fs := pasteApp(t, "")
	a.clipRead = func() (string, error) { return "", errors.New("boom") }
	press(t, a, keybind.NewRune('p', 0))
	if len(fs.writes) != 0 {
		t.Fatalf("p with clipboard error wrote %q", fs.writes)
	}
	if msg := a.statusMsg(); msg == "" {
		t.Fatal("expected a clipboard-error status message")
	}
}

func TestPasteCountCapped(t *testing.T) {
	// A large clipboard times a large count must not allocate the whole
	// product (OOM); the repeat count is clamped to maxPasteBytes.
	big := strings.Repeat("x", 64*1024)
	a, fs := pasteApp(t, big)
	pressDigits(t, a, "99999")
	press(t, a, keybind.NewRune('p', 0))
	// "abc" nudge: two left arrows to the virtual cursor.
	const nudge = 6
	pasted := len(fs.writes) - nudge
	if pasted <= 0 {
		t.Fatalf("p wrote %d bytes, want some paste text", len(fs.writes))
	}
	if pasted > maxPasteBytes {
		t.Fatalf("counted paste wrote %d bytes, want <= %d", pasted, maxPasteBytes)
	}
}