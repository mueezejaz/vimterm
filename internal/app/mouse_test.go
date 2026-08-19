package app

import (
	"testing"

	"vimterm/internal/console"
)

func TestMouseClickPositionsCursor(t *testing.T) {
	a := newMotionApp(t, 40, 5, "one\r\ntwo\r\nthree\r\n")
	a.handleMouse(console.MouseEvent{Button: console.MouseLeft, X: 1, Y: 1, Down: true})
	if a.cur.Line != 1 || a.cur.Col != 1 {
		t.Fatalf("click: cur = %+v, want line 1 col 1", a.cur)
	}
}

func TestMouseClickInScrollback(t *testing.T) {
	a := newMotionApp(t, 40, 5, "line 1\r\nline 2\r\nline 3\r\nline 4\r\nline 5\r\n")
	// Scroll up two lines so line 2 is on screen at row 1.
	a.vp.MoveUp(2)
	a.handleMouse(console.MouseEvent{Button: console.MouseLeft, X: 3, Y: 1, Down: true})
	if a.cur.Col != 3 {
		t.Fatalf("click: col = %d, want 3", a.cur.Col)
	}
	if a.cur.Line < 0 {
		t.Fatalf("click: line = %d, want >= 0", a.cur.Line)
	}
}

func TestMouseDragSelects(t *testing.T) {
	a := newMotionApp(t, 40, 5, "hello world\r\n")
	a.handleMouse(console.MouseEvent{Button: console.MouseLeft, X: 1, Y: 0, Down: true})
	a.handleMouse(console.MouseEvent{Button: console.MouseLeft, X: 4, Y: 0, Drag: true})
	if !a.sel.Active {
		t.Fatal("drag must create a selection")
	}
	s := a.sel.Start()
	e := a.sel.End()
	if s.Col != 1 || e.Col != 4 {
		t.Fatalf("selection %d..%d, want 1..4", s.Col, e.Col)
	}
}

func TestMouseWheelScrolls(t *testing.T) {
	a := newMotionApp(t, 40, 5, "line 1\r\nline 2\r\nline 3\r\nline 4\r\nline 5\r\n")
	a.handleMouse(console.MouseEvent{Button: console.MouseWheelUp, X: 0, Y: 0})
	if !a.vp.ScrolledUp() {
		t.Fatal("wheel up must scroll the viewport back")
	}
	a.handleMouse(console.MouseEvent{Button: console.MouseWheelDown, X: 0, Y: 0})
	if a.vp.ScrolledUp() {
		t.Fatal("wheel down must scroll the viewport forward again")
	}
}

func TestMouseDoubleClickSelectsWord(t *testing.T) {
	a := newMotionApp(t, 40, 5, "foo bar baz\r\n")
	a.handleMouse(console.MouseEvent{Button: console.MouseLeft, X: 5, Y: 0, Down: true, Double: true})
	if !a.sel.Active {
		t.Fatal("double click must create a selection")
	}
	s := a.sel.Start()
	e := a.sel.End()
	if s.Col != 4 || e.Col != 6 {
		t.Fatalf("word selection %d..%d, want 4..6", s.Col, e.Col)
	}
}

func TestMouseDoubleClickWordBoundaries(t *testing.T) {
	a := newMotionApp(t, 40, 5, "foo bar baz\r\n")
	for _, col := range []int{4, 5, 6} {
		a.handleMouse(console.MouseEvent{Button: console.MouseLeft, X: col, Y: 0, Down: true, Double: true})
		if !a.sel.Active {
			t.Fatalf("click at col %d: no selection", col)
		}
		s := a.sel.Start()
		e := a.sel.End()
		if s.Col != 4 || e.Col != 6 {
			t.Fatalf("click at col %d: word selection %d..%d, want 4..6", col, s.Col, e.Col)
		}
	}
}

func TestMouseDoubleClickOnSpaceFallsBackToClick(t *testing.T) {
	a := newMotionApp(t, 40, 5, "foo bar\r\n")
	a.handleMouse(console.MouseEvent{Button: console.MouseLeft, X: 3, Y: 0, Down: true, Double: true})
	if a.sel.Active {
		t.Fatal("double click on a space must not select")
	}
	if a.cur.Col != 3 {
		t.Fatalf("cursor = %d, want 3 (plain click)", a.cur.Col)
	}
}