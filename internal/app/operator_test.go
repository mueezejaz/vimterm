package app

// Tests for operator-pending mode: d/c/y + motion and text objects.

import (
	"testing"

	"vimterm/internal/keybind"
	"vimterm/internal/mode"
)

// pressAll feeds a string of rune keys through the key path.
func pressAll(t *testing.T, a *App, keys string) {
	t.Helper()
	for _, r := range keys {
		press(t, a, keybind.NewRune(r, 0))
	}
}

func TestOperatorDeleteToEndOfLine(t *testing.T) {
	a := findApp(t, "hello world\r\n")
	pressAll(t, a, "d$")
	if got := editLine(a, 0); got != "" {
		t.Fatalf("d$: line = %q, want empty", got)
	}
	if got := regText(a, 0); got != "hello world" {
		t.Fatalf("d$: register = %q, want %q", got, "hello world")
	}
	if a.mods.Current() != mode.ModeNormal {
		t.Fatalf("d$ left mode %v, want normal", a.mods.Current())
	}
}

func TestOperatorDeleteToColumnZero(t *testing.T) {
	a := findApp(t, "hello world\r\n")
	pressAll(t, a, "llld0")
	if got := editLine(a, 0); got != "lo world" {
		t.Fatalf("d0: line = %q, want %q", got, "lo world")
	}
	if got := regText(a, 0); got != "hel" {
		t.Fatalf("d0: register = %q, want %q", got, "hel")
	}
	if a.cur.Col != 0 {
		t.Fatalf("d0: col = %d, want 0", a.cur.Col)
	}
}

func TestOperatorChangeToEndOfLineEntersInsert(t *testing.T) {
	a, fs, _ := deleteApp(t, "hello world")
	pressAll(t, a, "c$")
	if a.mods.Current() != mode.ModeInsert {
		t.Fatalf("c$ left mode %v, want insert", a.mods.Current())
	}
	if got := editLine(a, 0); got != "" {
		t.Fatalf("c$: line = %q, want empty", got)
	}
	// The shell line was fully deleted: backspaces from the end.
	if len(fs.writes) == 0 {
		t.Fatalf("c$ wrote nothing to the shell")
	}
}

func TestOperatorYankWord(t *testing.T) {
	a, _, clip := deleteApp(t, "foo bar\r\n")
	pressAll(t, a, "yw")
	if got := regText(a, 0); got != "foo " {
		t.Fatalf("yw: register = %q, want %q", got, "foo ")
	}
	if got := editLine(a, 0); got != "foo bar" {
		t.Fatalf("yw must not modify the line, got %q", got)
	}
	if got := clip.String(); got != "foo " {
		t.Fatalf("yw: clipboard = %q, want %q", got, "foo ")
	}
}

func TestOperatorLinewiseDD(t *testing.T) {
	a := findApp(t, "first\r\nsecond\r\nthird\r\n")
	pressAll(t, a, "dd")
	if got := regText(a, 0); got != "first" {
		t.Fatalf("dd: register = %q, want %q", got, "first")
	}
	if got := editLine(a, 0); got != "" {
		t.Fatalf("dd: line 0 = %q, want blanked", got)
	}
}

func TestOperatorCountedDD(t *testing.T) {
	a := findApp(t, "one\r\ntwo\r\nthree\r\n")
	pressAll(t, a, "2dd")
	if got := regText(a, 0); got != "one\ntwo" {
		t.Fatalf("2dd: register = %q, want %q", got, "one\ntwo")
	}
}

func TestOperatorCountCompose(t *testing.T) {
	// 2d3w deletes 6 words' worth: "one two three four five six ".
	a := findApp(t, "one two three four five six rest\r\n")
	pressDigits(t, a, "2")
	pressAll(t, a, "d3w")
	if got := regText(a, 0); got != "one two three four five six " {
		t.Fatalf("2d3w: register = %q", got)
	}
	if got := editLine(a, 0); got != "rest" {
		t.Fatalf("2d3w: line = %q, want %q", got, "rest")
	}
}

func TestOperatorMotionAfterCountBeforeOperator(t *testing.T) {
	a := findApp(t, "one two three\r\n")
	pressAll(t, a, "d2w")
	if got := editLine(a, 0); got != "three" {
		t.Fatalf("d2w: line = %q, want %q", got, "three")
	}
}

func TestOperatorDeleteCharWiseLinewiseJ(t *testing.T) {
	a := findApp(t, "aa\r\nbb\r\ncc\r\n")
	pressAll(t, a, "dj")
	if got := regText(a, 0); got != "aa\nbb" {
		t.Fatalf("dj: register = %q, want %q", got, "aa\nbb")
	}
	if got := editLine(a, 0); got != "" {
		t.Fatalf("dj: line 0 = %q, want blanked", got)
	}
	if got := editLine(a, 1); got != "" {
		t.Fatalf("dj: line 1 = %q, want blanked", got)
	}
}

func TestOperatorFindTarget(t *testing.T) {
	a := findApp(t, "aXbXc\r\n")
	pressAll(t, a, "dfX")
	if got := editLine(a, 0); got != "bXc" {
		t.Fatalf("dfX: line = %q, want %q", got, "bXc")
	}
	if got := regText(a, 0); got != "aX" {
		t.Fatalf("dfX: register = %q, want %q", got, "aX")
	}
}

func TestOperatorFindUntilTarget(t *testing.T) {
	a := findApp(t, "aXbXc\r\n")
	pressAll(t, a, "dtX")
	if got := editLine(a, 0); got != "XbXc" {
		t.Fatalf("dtX: line = %q, want %q", got, "XbXc")
	}
}

func TestOperatorEscCancels(t *testing.T) {
	a := findApp(t, "hello\r\n")
	pressAll(t, a, "d")
	if a.mods.Current() != mode.ModeOperator {
		t.Fatalf("d did not enter operator mode, got %v", a.mods.Current())
	}
	press(t, a, keybind.NewCode(keybind.CodeEsc, 0))
	if a.mods.Current() != mode.ModeNormal {
		t.Fatalf("esc did not cancel operator mode, got %v", a.mods.Current())
	}
	if got := editLine(a, 0); got != "hello" {
		t.Fatalf("esc changed the line: %q", got)
	}
}

func TestOperatorDeadEndCancels(t *testing.T) {
	a := findApp(t, "hello\r\n")
	pressAll(t, a, "dz")
	if a.mods.Current() != mode.ModeNormal {
		t.Fatalf("dead-end key did not cancel operator mode, got %v", a.mods.Current())
	}
	if got := editLine(a, 0); got != "hello" {
		t.Fatalf("dead-end key changed the line: %q", got)
	}
}

func TestOperatorRepeatReplaysFullSequence(t *testing.T) {
	a := findApp(t, "one two three four\r\n")
	pressAll(t, a, "dw")
	if got := editLine(a, 0); got != "two three four" {
		t.Fatalf("dw: line = %q", got)
	}
	press(t, a, keybind.NewRune('.', 0))
	if got := editLine(a, 0); got != "three four" {
		t.Fatalf(". after dw: line = %q, want %q", got, "three four")
	}
}

func TestOperatorRepeatRestoresCount(t *testing.T) {
	a := findApp(t, "one two three four five six rest\r\n")
	pressDigits(t, a, "2")
	pressAll(t, a, "dw")
	if got := editLine(a, 0); got != "three four five six rest" {
		t.Fatalf("2dw: line = %q", got)
	}
	press(t, a, keybind.NewRune('.', 0))
	if got := editLine(a, 0); got != "five six rest" {
		t.Fatalf(". after 2dw: line = %q, want %q", got, "five six rest")
	}
}

func TestTextObjectInnerWord(t *testing.T) {
	a := findApp(t, "foo bar\r\n")
	pressAll(t, a, "diw")
	if got := regText(a, 0); got != "foo" {
		t.Fatalf("diw: register = %q, want %q", got, "foo")
	}
	if got := editLine(a, 0); got != " bar" {
		t.Fatalf("diw: line = %q, want %q", got, " bar")
	}
}

func TestTextObjectAroundWord(t *testing.T) {
	a := findApp(t, "foo bar\r\n")
	pressAll(t, a, "daw")
	if got := regText(a, 0); got != "foo " {
		t.Fatalf("daw: register = %q, want %q", got, "foo ")
	}
	if got := editLine(a, 0); got != "bar" {
		t.Fatalf("daw: line = %q, want %q", got, "bar")
	}
}

func TestTextObjectInnerQuotes(t *testing.T) {
	a := findApp(t, `say "hello there" ok`+"\r\n")
	// Cursor onto the 'e' of hello, then i".
	pressAll(t, a, "fe")
	pressAll(t, a, `di"`)
	if got := regText(a, 0); got != "hello there" {
		t.Fatalf(`di": register = %q, want %q`, got, "hello there")
	}
	if got := editLine(a, 0); got != `say "" ok` {
		t.Fatalf(`di": line = %q`, got)
	}
}

func TestTextObjectAroundQuotes(t *testing.T) {
	a := findApp(t, `say "hello" ok`+"\r\n")
	pressAll(t, a, "fe")
	pressAll(t, a, `da"`)
	if got := editLine(a, 0); got != `say  ok` {
		t.Fatalf(`da": line = %q`, got)
	}
}

func TestTextObjectInsideParens(t *testing.T) {
	a := findApp(t, "call(a, b) tail\r\n")
	pressAll(t, a, "f(") // cursor onto the '(' — inside its pair
	pressAll(t, a, "di(")
	if got := regText(a, 0); got != "a, b" {
		t.Fatalf("di(: register = %q, want %q", got, "a, b")
	}
	if got := editLine(a, 0); got != "call() tail" {
		t.Fatalf("di(: line = %q", got)
	}
}

func TestTextObjectAroundParens(t *testing.T) {
	a := findApp(t, "call(a, b) tail\r\n")
	pressAll(t, a, "f(")
	pressAll(t, a, "da(")
	if got := editLine(a, 0); got != "call tail" {
		t.Fatalf("da(: line = %q", got)
	}
}

func TestTextObjectFromInsideParens(t *testing.T) {
	a := findApp(t, "call(a, b) tail\r\n")
	pressAll(t, a, "f,") // cursor on the ',' strictly inside the pair
	pressAll(t, a, "di(")
	if got := editLine(a, 0); got != "call() tail" {
		t.Fatalf("di( from inside: line = %q", got)
	}
}

func TestOperatorWordEndInclusive(t *testing.T) {
	a := findApp(t, "one two three\r\n")
	pressAll(t, a, "de")
	if got := editLine(a, 0); got != " two three" {
		t.Fatalf("de: line = %q", got)
	}
	if got := regText(a, 0); got != "one" {
		t.Fatalf("de: register = %q, want %q", got, "one")
	}
}

func TestOperatorWORDMotion(t *testing.T) {
	// dW deletes through punctuation up to the next whitespace boundary.
	a := findApp(t, "a.b c d\r\n")
	pressAll(t, a, "dW")
	if got := editLine(a, 0); got != "c d" {
		t.Fatalf("dW: line = %q", got)
	}
	if got := regText(a, 0); got != "a.b " {
		t.Fatalf("dW: register = %q, want %q", got, "a.b ")
	}
}

func TestVisualDeleteUsesRegisters(t *testing.T) {
	a := findApp(t, "hello world\r\n")
	press(t, a, keybind.NewRune('v', 0))
	pressAll(t, a, "llll") // select "hello"
	if a.mods.Current() != mode.ModeVisual {
		t.Fatalf("v did not enter visual mode")
	}
	press(t, a, keybind.NewRune('d', 0))
	if got := regText(a, 0); got != "hello" {
		t.Fatalf("visual d: register = %q, want %q", got, "hello")
	}
	if got := editLine(a, 0); got != " world" {
		t.Fatalf("visual d: line = %q, want %q", got, " world")
	}
	if a.mods.Current() != mode.ModeNormal {
		t.Fatalf("visual d left mode %v", a.mods.Current())
	}
}
