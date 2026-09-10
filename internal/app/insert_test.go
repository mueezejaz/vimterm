package app

import (
	"testing"

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
