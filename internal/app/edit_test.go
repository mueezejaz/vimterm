package app

import (
	"strings"
	"testing"

	"vimterm/internal/keybind"
)

func editApp(t *testing.T, content string) (*App, *strings.Builder) {
	t.Helper()
	a := findApp(t, content)
	var clip strings.Builder
	a.clipWrite = func(s string) error {
		clip.WriteString(s)
		return nil
	}
	return a, &clip
}

// deleteApp builds an app with a fake session so delete propagation to the
// shell can be observed. Content must leave the shell cursor on line 0 (no
// trailing CRLF) for the shell line to match.
func deleteApp(t *testing.T, content string) (*App, *fakeSession, *strings.Builder) {
	t.Helper()
	a, clip := editApp(t, content)
	fs := &fakeSession{}
	a.sess = fs
	return a, fs, clip
}

func TestDeleteWordForwardPropagatesToShell(t *testing.T) {
	// "foo bar baz" (11 cols, no CRLF) leaves the shell cursor at (11, 0).
	a, fs, _ := deleteApp(t, "foo bar baz")
	press(t, a, keybind.NewRune('d', 0))
	press(t, a, keybind.NewRune('w', 0))
	// Move the shell cursor left by 11-4=7, then delete 4 cells with BS.
	want := strings.Repeat("\x1b[D", 7) + strings.Repeat("\x7f", 4)
	if got := string(fs.writes); got != want {
		t.Fatalf("dw propagated %q, want %q", got, want)
	}
}

func TestDeleteWordBackPropagatesToShell(t *testing.T) {
	a, fs, _ := deleteApp(t, "foo bar baz")
	a.cur.Col = 6
	press(t, a, keybind.NewRune('d', 0))
	press(t, a, keybind.NewRune('b', 0))
	// db deletes [4,6): move shell cursor to col 6 (5 left), 2 backspaces.
	want := strings.Repeat("\x1b[D", 5) + strings.Repeat("\x7f", 2)
	if got := string(fs.writes); got != want {
		t.Fatalf("db propagated %q, want %q", got, want)
	}
}

func TestDeleteWordForwardCountPropagatesToShell(t *testing.T) {
	// 2dw from col 0 on "one two three": segments [0,4) and [4,8) merge
	// into [0,8) on the shell line. Shell cursor at 13, move left 5,
	// delete 8.
	a, fs, _ := deleteApp(t, "one two three")
	press(t, a, keybind.NewRune('2', 0))
	press(t, a, keybind.NewRune('d', 0))
	press(t, a, keybind.NewRune('w', 0))
	want := strings.Repeat("\x1b[D", 5) + strings.Repeat("\x7f", 8)
	if got := string(fs.writes); got != want {
		t.Fatalf("2dw propagated %q, want %q", got, want)
	}
}

func TestDeleteWordNoPropagationOnOtherLine(t *testing.T) {
	// CRLF moves the shell cursor to line 1, but the deletion is on line 0.
	a, fs, _ := deleteApp(t, "foo bar baz\r\n")
	press(t, a, keybind.NewRune('d', 0))
	press(t, a, keybind.NewRune('w', 0))
	if len(fs.writes) != 0 {
		t.Fatalf("dw on another line wrote %q, want nothing", fs.writes)
	}
}

func editLine(a *App, line int) string {
	return strings.TrimRight(string(a.bufferLine(line)), " ")
}

func TestYankLine(t *testing.T) {
	a, clip := editApp(t, "hello world\r\nsecond line\r\n")
	press(t, a, keybind.NewRune('y', 0))
	press(t, a, keybind.NewRune('y', 0))
	if got := clip.String(); got != "hello world" {
		t.Fatalf("yy: clipboard = %q, want %q", got, "hello world")
	}
}

func TestYankLineCount(t *testing.T) {
	a, clip := editApp(t, "one\r\ntwo\r\nthree\r\n")
	for _, k := range []rune{'2', 'y', 'y'} {
		press(t, a, keybind.NewRune(k, 0))
	}
	if got := clip.String(); got != "one\ntwo" {
		t.Fatalf("2yy: clipboard = %q, want %q", got, "one\ntwo")
	}
	if a.cur.Line != 0 {
		t.Fatalf("2yy: cur.Line = %d, want 0", a.cur.Line)
	}
}

func TestYankLinePreservesBlankLines(t *testing.T) {
	a, clip := editApp(t, "a\r\n\r\nb\r\n")
	for _, k := range []rune{'3', 'y', 'y'} {
		press(t, a, keybind.NewRune(k, 0))
	}
	if got := clip.String(); got != "a\n\nb" {
		t.Fatalf("3yy with blank line: clipboard = %q, want %q", got, "a\n\nb")
	}
}

func TestYankLineSingleBlankLine(t *testing.T) {
	a, clip := editApp(t, "\r\nsecond\r\n")
	// Move to line 0 (blank line) and yank it.
	press(t, a, keybind.NewRune('y', 0))
	press(t, a, keybind.NewRune('y', 0))
	if got := clip.String(); got != "" {
		t.Fatalf("yy blank line: clipboard = %q, want empty", got)
	}
}

func TestDeleteWordForward(t *testing.T) {
	a, clip := editApp(t, "foo bar baz\r\n")
	press(t, a, keybind.NewRune('d', 0))
	press(t, a, keybind.NewRune('w', 0))
	if got := clip.String(); got != "foo " {
		t.Fatalf("dw: clipboard = %q, want %q", got, "foo ")
	}
	if got := editLine(a, 0); got != "bar baz" {
		t.Fatalf("dw: line = %q, want %q", got, "bar baz")
	}
	if a.cur.Col != 0 {
		t.Fatalf("dw: col = %d, want 0", a.cur.Col)
	}
}

func TestDeleteWordMidWord(t *testing.T) {
	a, clip := editApp(t, "foo bar baz\r\n")
	a.cur.Col = 2
	press(t, a, keybind.NewRune('d', 0))
	press(t, a, keybind.NewRune('w', 0))
	if got := clip.String(); got != "o " {
		t.Fatalf("dw mid-word: clipboard = %q, want %q", got, "o ")
	}
	if got := editLine(a, 0); got != "fobar baz" {
		t.Fatalf("dw mid-word: line = %q, want %q", got, "fobar baz")
	}
	if a.cur.Col != 2 {
		t.Fatalf("dw mid-word: col = %d, want 2", a.cur.Col)
	}
}

func TestDeleteWordLastWord(t *testing.T) {
	a, clip := editApp(t, "foo bar\r\n")
	a.cur.Col = 4
	press(t, a, keybind.NewRune('d', 0))
	press(t, a, keybind.NewRune('w', 0))
	if got := clip.String(); got != "bar" {
		t.Fatalf("dw last word: clipboard = %q, want %q", got, "bar")
	}
	if got := editLine(a, 0); got != "foo" {
		t.Fatalf("dw last word: line = %q, want %q", got, "foo")
	}
	if a.cur.Col != 4 {
		t.Fatalf("dw last word: col = %d, want 4", a.cur.Col)
	}
}

func TestDeleteWordForwardCount(t *testing.T) {
	a, clip := editApp(t, "one two three\r\n")
	for _, k := range []rune{'2', 'd', 'w'} {
		press(t, a, keybind.NewRune(k, 0))
	}
	if got := clip.String(); got != "one two " {
		t.Fatalf("2dw: clipboard = %q, want %q", got, "one two ")
	}
	if got := editLine(a, 0); got != "three" {
		t.Fatalf("2dw: line = %q, want %q", got, "three")
	}
}

func TestDeleteWordBack(t *testing.T) {
	a, clip := editApp(t, "foo bar baz\r\n")
	a.cur.Col = 6
	press(t, a, keybind.NewRune('d', 0))
	press(t, a, keybind.NewRune('b', 0))
	if got := clip.String(); got != "ba" {
		t.Fatalf("db: clipboard = %q, want %q", got, "ba")
	}
	if got := editLine(a, 0); got != "foo r baz" {
		t.Fatalf("db: line = %q, want %q", got, "foo r baz")
	}
	if a.cur.Col != 4 {
		t.Fatalf("db: col = %d, want 4", a.cur.Col)
	}
}

func TestDeleteWordBackAtWordStart(t *testing.T) {
	a, clip := editApp(t, "foo bar baz\r\n")
	a.cur.Col = 4
	press(t, a, keybind.NewRune('d', 0))
	press(t, a, keybind.NewRune('b', 0))
	if got := clip.String(); got != "foo " {
		t.Fatalf("db at word start: clipboard = %q, want %q", got, "foo ")
	}
	if got := editLine(a, 0); got != "bar baz" {
		t.Fatalf("db at word start: line = %q, want %q", got, "bar baz")
	}
	if a.cur.Col != 0 {
		t.Fatalf("db at word start: col = %d, want 0", a.cur.Col)
	}
}

func TestDeleteWordBackNothingAtLineStart(t *testing.T) {
	a, clip := editApp(t, "foo\r\n")
	press(t, a, keybind.NewRune('d', 0))
	press(t, a, keybind.NewRune('b', 0))
	if got := clip.String(); got != "" {
		t.Fatalf("db at line start: clipboard = %q, want empty", got)
	}
	if got := editLine(a, 0); got != "foo" {
		t.Fatalf("db at line start: line = %q, want %q", got, "foo")
	}
}

func TestDeleteWordForwardCrossesLines(t *testing.T) {
	a, clip := editApp(t, "foo\r\nbar baz\r\n")
	a.cur.Col = 1
	for _, k := range []rune{'2', 'd', 'w'} {
		press(t, a, keybind.NewRune(k, 0))
	}
	if got := clip.String(); got != "oo\nbar " {
		t.Fatalf("2dw across lines: clipboard = %q, want %q", got, "oo\nbar ")
	}
	if got := editLine(a, 0); got != "f" {
		t.Fatalf("2dw across lines: line 0 = %q, want %q", got, "f")
	}
	if got := editLine(a, 1); got != "baz" {
		t.Fatalf("2dw across lines: line 1 = %q, want %q", got, "baz")
	}
}

func TestDeleteWordInScrollback(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString("word line ")
		sb.WriteString(itoa(i))
		sb.WriteString("\r\n")
	}
	a := findApp(t, sb.String())
	if a.emu.ScrollbackLen() == 0 {
		t.Fatal("expected content to scroll off into scrollback")
	}
	var clip strings.Builder
	a.clipWrite = func(s string) error {
		clip.WriteString(s)
		return nil
	}
	press(t, a, keybind.NewRune('d', 0))
	press(t, a, keybind.NewRune('w', 0))
	if got := clip.String(); got != "word " {
		t.Fatalf("dw in scrollback: clipboard = %q, want %q", got, "word ")
	}
	if got := editLine(a, 0); got != "line 0" {
		t.Fatalf("dw in scrollback: line 0 = %q, want %q", got, "line 0")
	}
}