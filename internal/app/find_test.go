package app

import (
	"testing"

	"vimterm/internal/keybind"
)

func TestFindOnLine(t *testing.T) {
	line := []rune("abcabc")
	cases := []struct {
		name  string
		col   int
		dir   int
		until bool
		ch    rune
		count int
		want  int
	}{
		{"f finds next", 0, 1, false, 'b', 1, 1},
		{"f skips to later occurrence", 1, 1, false, 'a', 1, 3},
		{"f past end not found", 4, 1, false, 'c', 1, 5},
		{"f not found", 0, 1, false, 'x', 1, -1},
		{"t stops before", 0, 1, true, 'b', 1, 0},
		{"t before later occurrence", 1, 1, true, 'a', 1, 2},
		{"F finds previous", 4, -1, false, 'b', 1, 1},
		{"F not found", 1, -1, false, 'c', 1, -1},
		{"T stops after", 4, -1, true, 'b', 1, 2},
		{"T after later occurrence", 4, -1, true, 'a', 1, 4},
		{"f count finds second", 0, 1, false, 'b', 2, 4},
		{"f count beyond end not found", 0, 1, false, 'c', 4, -1},
		{"t count stops before second", 0, 1, true, 'b', 2, 3},
		{"F count finds second backward", 5, -1, false, 'b', 2, 1},
	}
	for _, c := range cases {
		got := findOnLine(line, c.col, c.dir, c.until, c.ch, c.count, false)
		if got != c.want {
			t.Errorf("%s: findOnLine = %d, want %d", c.name, got, c.want)
		}
	}
}

func findApp(t *testing.T, content string) *App {
	a := newMotionApp(t, 40, 5, content)
	// Anchor at the top so the cursor sits at the first line, col 0.
	press(t, a, keybind.NewRune('g', 0))
	press(t, a, keybind.NewRune('g', 0))
	return a
}

func TestFindCharMovesCursor(t *testing.T) {
	a := findApp(t, "abc abc\r\n")
	press(t, a, keybind.NewRune('f', 0))
	press(t, a, keybind.NewRune('b', 0))
	if a.cur.Col != 1 {
		t.Fatalf("f b: col = %d, want 1", a.cur.Col)
	}
	press(t, a, keybind.NewRune('f', 0))
	press(t, a, keybind.NewRune('a', 0))
	if a.cur.Col != 4 {
		t.Fatalf("f a: col = %d, want 4", a.cur.Col)
	}
	if a.cur.Line != 0 {
		t.Fatalf("f must stay on the current line, got line %d", a.cur.Line)
	}
}

func TestFindCharBackward(t *testing.T) {
	a := findApp(t, "abc abc\r\n")
	press(t, a, keybind.NewRune('f', 0))
	press(t, a, keybind.NewRune('c', 0))
	press(t, a, keybind.NewRune('F', keybind.ModShift))
	press(t, a, keybind.NewRune('a', 0))
	if a.cur.Col != 0 {
		t.Fatalf("F a: col = %d, want 0 (nearest backward)", a.cur.Col)
	}
}

func TestFindUntil(t *testing.T) {
	a := findApp(t, "abc\r\n")
	press(t, a, keybind.NewRune('t', 0))
	press(t, a, keybind.NewRune('c', 0))
	if a.cur.Col != 1 {
		t.Fatalf("t c: col = %d, want 1", a.cur.Col)
	}
	press(t, a, keybind.NewRune('T', keybind.ModShift))
	press(t, a, keybind.NewRune('a', 0))
	if a.cur.Col != 1 {
		t.Fatalf("T a: col = %d, want 1", a.cur.Col)
	}
}

func TestFindNotFound(t *testing.T) {
	a := findApp(t, "abc\r\n")
	press(t, a, keybind.NewRune('f', 0))
	press(t, a, keybind.NewRune('x', 0))
	if a.cur.Col != 0 {
		t.Fatalf("f x: col = %d, want 0 (no move)", a.cur.Col)
	}
	if msg := a.statusMsg(); msg == "" {
		t.Fatal("expected a not-found status message")
	}
}

func TestFindRepeatSemicolonComma(t *testing.T) {
	a := findApp(t, "a-b-c-d-e\r\n")
	press(t, a, keybind.NewRune('f', 0))
	press(t, a, keybind.NewRune('-', 0))
	if a.cur.Col != 1 {
		t.Fatalf("f -: col = %d, want 1", a.cur.Col)
	}
	press(t, a, keybind.NewRune(';', 0))
	press(t, a, keybind.NewRune(';', 0))
	if a.cur.Col != 5 {
		t.Fatalf(";;: col = %d, want 5", a.cur.Col)
	}
	press(t, a, keybind.NewRune(',', 0))
	if a.cur.Col != 3 {
		t.Fatalf(",: col = %d, want 3", a.cur.Col)
	}
	press(t, a, keybind.NewRune(',', 0))
	if a.cur.Col != 1 {
		t.Fatalf(",,: col = %d, want 1 (keeps marching backward)", a.cur.Col)
	}
	press(t, a, keybind.NewRune(',', 0))
	if a.cur.Col != 1 {
		t.Fatalf(",,,: col = %d, want 1 (no match left)", a.cur.Col)
	}
	press(t, a, keybind.NewRune(';', 0))
	if a.cur.Col != 3 {
		t.Fatalf("; after ,,: col = %d, want 3 (forward again)", a.cur.Col)
	}
}

func TestFindRepeatAfterT(t *testing.T) {
	a := findApp(t, "a,b,c,d,e\r\n")
	press(t, a, keybind.NewRune('t', 0))
	press(t, a, keybind.NewRune(',', 0))
	if a.cur.Col != 0 {
		t.Fatalf("t ,: col = %d, want 0", a.cur.Col)
	}
	press(t, a, keybind.NewRune(';', 0))
	if a.cur.Col != 2 {
		t.Fatalf("; after t: col = %d, want 2", a.cur.Col)
	}
	press(t, a, keybind.NewRune(';', 0))
	if a.cur.Col != 4 {
		t.Fatalf(";; after t: col = %d, want 4", a.cur.Col)
	}
	press(t, a, keybind.NewRune(';', 0))
	if a.cur.Col != 6 {
		t.Fatalf(";;; after t: col = %d, want 6", a.cur.Col)
	}
	press(t, a, keybind.NewRune(',', 0))
	if a.cur.Col != 4 {
		t.Fatalf(", after t-chain: col = %d, want 4 (skips the adjacent match)", a.cur.Col)
	}
	press(t, a, keybind.NewRune(',', 0))
	if a.cur.Col != 2 {
		t.Fatalf(",, after t-chain: col = %d, want 2", a.cur.Col)
	}
	press(t, a, keybind.NewRune(',', 0))
	if a.cur.Col != 2 {
		t.Fatalf(",,, after t-chain: col = %d, want 2 (no match left)", a.cur.Col)
	}
}

func TestFindRepeatAfterTWithCount(t *testing.T) {
	// Vim counts the adjacent match for a counted repeat: 2; after t,
	// lands on the same column as ; .
	a := findApp(t, "a,b,c,d,e\r\n")
	press(t, a, keybind.NewRune('t', 0))
	press(t, a, keybind.NewRune(',', 0))
	press(t, a, keybind.NewRune('2', 0))
	press(t, a, keybind.NewRune(';', 0))
	if a.cur.Col != 2 {
		t.Fatalf("2; after t: col = %d, want 2", a.cur.Col)
	}
}

func TestFindRepeatAfterTBackward(t *testing.T) {
	a := findApp(t, "a,b,c,d,e\r\n")
	for i := 0; i < 4; i++ {
		press(t, a, keybind.NewRune('f', 0))
		press(t, a, keybind.NewRune(',', 0))
	}
	if a.cur.Col != 7 {
		t.Fatalf("f-chain: col = %d, want 7", a.cur.Col)
	}
	press(t, a, keybind.NewRune('T', keybind.ModShift))
	press(t, a, keybind.NewRune(',', 0))
	if a.cur.Col != 6 {
		t.Fatalf("T ,: col = %d, want 6", a.cur.Col)
	}
	press(t, a, keybind.NewRune(';', 0))
	if a.cur.Col != 4 {
		t.Fatalf("; after T: col = %d, want 4", a.cur.Col)
	}
	press(t, a, keybind.NewRune(';', 0))
	if a.cur.Col != 2 {
		t.Fatalf(";; after T: col = %d, want 2", a.cur.Col)
	}
	press(t, a, keybind.NewRune(';', 0))
	if a.cur.Col != 2 {
		t.Fatalf(";;; after T: col = %d, want 2 (no match left)", a.cur.Col)
	}
}

func TestFindWithoutPrevious(t *testing.T) {
	a := findApp(t, "abc\r\n")
	press(t, a, keybind.NewRune(';', 0))
	if msg := a.statusMsg(); msg == "" {
		t.Fatal("expected 'no previous find' status message")
	}
	if a.cur.Col != 0 {
		t.Fatalf("; without previous find must not move, got col %d", a.cur.Col)
	}
}

func TestFindPendingCanceledByNonRune(t *testing.T) {
	a := findApp(t, "abc\r\n")
	press(t, a, keybind.NewRune('f', 0))
	// Esc cancels the pending find and is then handled normally.
	press(t, a, keybind.NewCode(keybind.CodeEsc, 0))
	if a.find.pending() {
		t.Fatal("pending find must be canceled by a non-rune key")
	}
	if a.cur.Col != 0 {
		t.Fatalf("cursor must not move, got col %d", a.cur.Col)
	}
}

func TestFindInVisualExtendsSelection(t *testing.T) {
	a := findApp(t, "abc\r\n")
	press(t, a, keybind.NewRune('v', 0))
	press(t, a, keybind.NewRune('f', 0))
	press(t, a, keybind.NewRune('c', 0))
	if !a.sel.Active {
		t.Fatal("selection must be active")
	}
	if a.cur.Col != 2 {
		t.Fatalf("f c in visual: col = %d, want 2", a.cur.Col)
	}
	if a.sel.End().Col != 2 {
		t.Fatalf("selection must end at col 2, got %d", a.sel.End().Col)
	}
}