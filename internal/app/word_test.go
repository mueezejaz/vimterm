package app

import (
	"testing"

	"vimterm/internal/keybind"
)

func TestWordStart(t *testing.T) {
	line := []rune("foo bar baz")
	cases := []struct {
		col, dir, kind int
		want           int
	}{
		{0, 1, 0, 4},   // w from first word
		{4, 1, 0, 8},   // w from second word start
		{3, 1, 0, 4},   // w from space
		{8, -1, 0, 4},  // b from third word start
		{5, -1, 0, 4},  // b from inside second word
		{4, -1, 0, 0},  // b from word start: previous word
		{1, -1, 0, 0},  // b from inside first word
		{0, -1, 0, -1}, // b at buffer start: no word before
	}
	for _, c := range cases {
		if got := wordStart(line, c.col, c.dir, c.kind); got != c.want {
			t.Errorf("wordStart(%d,%d,kind %d) = %d, want %d", c.col, c.dir, c.kind, got, c.want)
		}
	}
}

func TestWordEnd(t *testing.T) {
	line := []rune("foo bar baz")
	cases := []struct {
		col, kind int
		want      int
	}{
		{0, 0, 2}, // e from first word
		{2, 0, 6}, // e from word end (moves on)
		{4, 0, 6}, // e from inside second word
		{7, 0, 10},
	}
	for _, c := range cases {
		if got := wordEnd(line, c.col, c.kind); got != c.want {
			t.Errorf("wordEnd(%d, kind %d) = %d, want %d", c.col, c.kind, got, c.want)
		}
	}
}

func TestWordWORDKind(t *testing.T) {
	line := []rune("foo.bar baz")
	if got := wordStart(line, 0, 1, wordKindWORD); got != 8 {
		t.Errorf("W from 0 = %d, want 8", got)
	}
	if got := wordStart(line, 0, 1, wordKindWord); got != 4 {
		t.Errorf("w from 0 = %d, want 4", got)
	}
}

func TestWordMotionWBE(t *testing.T) {
	a := findApp(t, "foo bar baz\r\n")
	press(t, a, keybind.NewRune('w', 0))
	if a.cur.Col != 4 {
		t.Fatalf("w: col = %d, want 4", a.cur.Col)
	}
	press(t, a, keybind.NewRune('w', 0))
	if a.cur.Col != 8 {
		t.Fatalf("w: col = %d, want 8", a.cur.Col)
	}
	press(t, a, keybind.NewRune('e', 0))
	if a.cur.Col != 10 {
		t.Fatalf("e: col = %d, want 10", a.cur.Col)
	}
	press(t, a, keybind.NewRune('b', 0))
	if a.cur.Col != 8 {
		t.Fatalf("b from word end: col = %d, want 8", a.cur.Col)
	}
	press(t, a, keybind.NewRune('b', 0))
	if a.cur.Col != 4 {
		t.Fatalf("b: col = %d, want 4", a.cur.Col)
	}
	press(t, a, keybind.NewRune('e', 0))
	if a.cur.Col != 6 {
		t.Fatalf("e from word start: col = %d, want 6", a.cur.Col)
	}
	press(t, a, keybind.NewRune('e', 0))
	if a.cur.Col != 10 {
		t.Fatalf("e: col = %d, want 10", a.cur.Col)
	}
}

func TestWordMotionCrossesLines(t *testing.T) {
	a := findApp(t, "foo\r\n  bar baz\r\n")
	press(t, a, keybind.NewRune('e', 0))
	if a.cur.Col != 2 || a.cur.Line != 0 {
		t.Fatalf("e at end of line: col %d line %d, want 2/0", a.cur.Col, a.cur.Line)
	}
	press(t, a, keybind.NewRune('e', 0))
	if a.cur.Col != 4 || a.cur.Line != 1 {
		t.Fatalf("e across lines: col %d line %d, want 4/1", a.cur.Col, a.cur.Line)
	}
	press(t, a, keybind.NewRune('b', 0))
	if a.cur.Col != 2 || a.cur.Line != 1 {
		t.Fatalf("b to word start: col %d line %d, want 2/1", a.cur.Col, a.cur.Line)
	}
	press(t, a, keybind.NewRune('b', 0))
	if a.cur.Col != 0 || a.cur.Line != 0 {
		t.Fatalf("b across lines: col %d line %d, want 0/0", a.cur.Col, a.cur.Line)
	}
}

func TestCountWordMotion(t *testing.T) {
	a := findApp(t, "one two three four\r\n")
	pressDigits(t, a, "3")
	press(t, a, keybind.NewRune('w', 0))
	if a.cur.Col != 14 {
		t.Fatalf("3w: col = %d, want 14", a.cur.Col)
	}
}

func TestWordMotionInVisual(t *testing.T) {
	a := findApp(t, "one two\r\n")
	press(t, a, keybind.NewRune('v', 0))
	press(t, a, keybind.NewRune('w', 0))
	if !a.sel.Active {
		t.Fatal("selection must be active")
	}
	if a.cur.Col != 4 {
		t.Fatalf("v w: col = %d, want 4", a.cur.Col)
	}
}