package app

import (
	"testing"

	"vimterm/internal/keybind"
)

func digit(t *testing.T, d rune) keybind.Key {
	t.Helper()
	return keybind.NewRune(d, 0)
}

func pressDigits(t *testing.T, a *App, digits string) {
	t.Helper()
	for _, d := range digits {
		press(t, a, digit(t, d))
	}
}

func TestCountMoveDown(t *testing.T) {
	a := findApp(t, "one\r\ntwo\r\nthree\r\nfour\r\n")
	pressDigits(t, a, "3")
	press(t, a, keybind.NewRune('j', 0))
	if a.cur.Line != 3 {
		t.Fatalf("3j: line = %d, want 3", a.cur.Line)
	}
}

func TestCountMoveClamps(t *testing.T) {
	a := findApp(t, "one\r\n")
	pressDigits(t, a, "9")
	press(t, a, keybind.NewRune('j', 0))
	if a.cur.Line != 4 {
		t.Fatalf("9j on one line: line = %d, want 4 (buffer bottom)", a.cur.Line)
	}
}

func TestCountGotoLine(t *testing.T) {
	a := findApp(t, "one\r\ntwo\r\nthree\r\nfour\r\n")
	pressDigits(t, a, "3")
	press(t, a, keybind.NewRune('g', 0))
	press(t, a, keybind.NewRune('g', 0))
	if a.cur.Line != 2 {
		t.Fatalf("3gg: line = %d, want 2", a.cur.Line)
	}
}

func TestCountGotoBottom(t *testing.T) {
	a := findApp(t, "one\r\ntwo\r\nthree\r\nfour\r\n")
	pressDigits(t, a, "2")
	press(t, a, keybind.NewRune('G', keybind.ModShift))
	if a.cur.Line != 1 {
		t.Fatalf("2G: line = %d, want 1", a.cur.Line)
	}
}

func TestCountFind(t *testing.T) {
	a := findApp(t, "a-b-c-d\r\n")
	pressDigits(t, a, "3")
	press(t, a, keybind.NewRune('f', 0))
	press(t, a, keybind.NewRune('-', 0))
	if a.cur.Col != 5 {
		t.Fatalf("3f-: col = %d, want 5", a.cur.Col)
	}
}

func TestCountRepeatFind(t *testing.T) {
	a := findApp(t, "a-b-c-d\r\n")
	press(t, a, keybind.NewRune('f', 0))
	press(t, a, keybind.NewRune('-', 0))
	pressDigits(t, a, "2")
	press(t, a, keybind.NewRune(';', 0))
	if a.cur.Col != 5 {
		t.Fatalf("f- 2;: col = %d, want 5", a.cur.Col)
	}
}

func TestCountResetByNonCountAction(t *testing.T) {
	a := findApp(t, "one\r\ntwo\r\nthree\r\n")
	pressDigits(t, a, "3")
	press(t, a, keybind.NewRune('i', 0))
	if a.count != 0 {
		t.Fatal("count must reset after a non-count-aware action")
	}
}

func TestCountLeadingZeroIgnored(t *testing.T) {
	a := findApp(t, "one\r\n")
	press(t, a, digit(t, '0'))
	if a.count != 0 {
		t.Fatal("leading 0 must not start a count")
	}
}

func TestCountCanceledByEsc(t *testing.T) {
	a := findApp(t, "one\r\n")
	pressDigits(t, a, "3")
	press(t, a, keybind.NewCode(keybind.CodeEsc, 0))
	if a.count != 0 {
		t.Fatal("count must reset on an unbound key")
	}
}
