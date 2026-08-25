package app

// Regression tests: cursor columns are cell columns, but motions scan the
// flattened rune text. Wide characters occupy two cells per one rune, so
// every conversion below used to mix the two coordinate systems.

import (
	"testing"

	"vimterm/internal/selection"
)

func wideApp(t *testing.T, content string) *App {
	t.Helper()
	a := realApp(t, 20, 6, content)
	a.clipRead = func() (string, error) { return "", nil }
	a.clipWrite = func(string) error { return nil }
	a.cur = selection.Pos{Line: 0, Col: 0}
	a.curValid = true
	return a
}

func TestFindOverWideChars(t *testing.T) {
	// Cells: 你(0-1) x(2) 你(3-4) y(5); runes [你 x 你 y].
	a := wideApp(t, "你x你y\r\n")
	a.doFind(1, false, 'y')
	if a.cur.Col != 5 {
		t.Fatalf("f y col = %d, want cell 5", a.cur.Col)
	}
	// Backward find from past the end lands on the second 你's lead cell.
	a.cur.Col = 6
	a.curValid = true
	a.executeFind(-1, false, '你', 1, false)
	if a.cur.Col != 3 {
		t.Fatalf("F 你 col = %d, want cell 3", a.cur.Col)
	}
}

func TestWordMotionOverWideChars(t *testing.T) {
	// Cells: ab(0,1) sp(2) 你(3,4) sp(5) cd(6,7). The engine's w skips
	// non-word runs (the wide char included), so it must cross the two
	// extra wide cells and land on 'c'; b returns across them.
	a := wideApp(t, "ab 你 cd\r\n")
	a.wordMotion(1, wordKindWord)
	if a.cur.Col != 6 {
		t.Fatalf("first w col = %d, want cell 6", a.cur.Col)
	}
	a.wordMotion(-1, wordKindWord)
	if a.cur.Col != 0 {
		t.Fatalf("b col = %d, want cell 0", a.cur.Col)
	}
}

func TestWordEndMotionSkipsWideChar(t *testing.T) {
	// Cells: 你(0-1) ab(2,3): e lands on 'b' at cell 3.
	a := wideApp(t, "你ab\r\n")
	a.wordEndMotion(wordKindWORD)
	if a.cur.Col != 3 {
		t.Fatalf("e col = %d, want cell 3", a.cur.Col)
	}
}

func TestInsertAfterWideChar(t *testing.T) {
	a := wideApp(t, "你x\r\n")
	a.enterInsertAfter()
	if a.cur.Col != 2 {
		t.Fatalf("a on wide char: col = %d, want cell 2 (not mid-char)", a.cur.Col)
	}
}

func TestInsertEndWideChar(t *testing.T) {
	a := wideApp(t, "ab你\r\n")
	a.enterInsertEnd()
	if a.cur.Col != 4 {
		t.Fatalf("A: col = %d, want cell 4", a.cur.Col)
	}
}

func TestDeleteWordWideChar(t *testing.T) {
	// dw over the leading wide character removes both of its cells (plus
	// the space, up to the next word start); deleting a single cell would
	// split the glyph in half.
	a := wideApp(t, "你 go\r\n")
	a.deleteWord(1)
	if got := a.emu.Cell(0, 0).Content; got != "g" {
		t.Fatalf("cell 0 = %q, want g", got)
	}
	if got := a.emu.Cell(1, 0).Content; got != "o" {
		t.Fatalf("cell 1 = %q, want o", got)
	}
	if a.cur.Col != 0 {
		t.Fatalf("cursor col = %d, want 0", a.cur.Col)
	}
}

func TestDeleteLineCellsWideIntact(t *testing.T) {
	a := wideApp(t, "你x\r\n")
	line := a.bufferLineCells(0)
	row := rowOf(line)
	cellFrom, cellTo := row.colAt(0), row.colAt(1)
	a.emu.DeleteLineCells(0, cellFrom, cellTo-cellFrom)
	if got := a.emu.Cell(0, 0).Content; got != "x" {
		t.Fatalf("cell 0 = %q, want x", got)
	}
}

func TestYankSelectionWideChars(t *testing.T) {
	a := wideApp(t, "你ab\r\n")
	a.sel.Begin(selection.Pos{Line: 0, Col: 1}) // continuation cell of 你
	a.sel.Move(selection.Pos{Line: 0, Col: 3})
	if text := a.sel.Text(a.bufferLineRow); text != "你ab" {
		t.Fatalf("yank = %q, want 你ab", text)
	}
}
