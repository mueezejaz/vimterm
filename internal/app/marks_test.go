package app

// Tests for marks: m{char} sets, `{char} jumps exactly, '{char} jumps to the
// first non-blank column.

import "testing"

func TestMarkSetAndJumpExact(t *testing.T) {
	a := findApp(t, "one two three\r\nsecond line\r\n")
	pressAll(t, a, "ma")  // mark a at (0,0)
	pressAll(t, a, "llw") // wander off: (0,8)? l,l → col 2; w → next word
	pressAll(t, a, "`a")
	if a.cur.Line != 0 || a.cur.Col != 0 {
		t.Fatalf("`a: cursor = %v, want (0,0)", a.cur)
	}
}

func TestMarkJumpLineWise(t *testing.T) {
	a := findApp(t, "one two three\r\n   second line\r\n")
	pressAll(t, a, "w")  // onto "two"
	pressAll(t, a, "mb") // mark b mid-line
	pressAll(t, a, "j$") // down a line, end of line
	pressAll(t, a, "'b")
	if a.cur.Line != 0 {
		t.Fatalf("'b: line = %d, want 0", a.cur.Line)
	}
	if a.cur.Col != 0 {
		t.Fatalf("'b: col = %d, want first non-blank (0)", a.cur.Col)
	}
}

func TestMarkJumpFirstNonBlank(t *testing.T) {
	a := findApp(t, "one two\r\n    indented\r\n")
	pressAll(t, a, "ma") // mark on line 0
	pressAll(t, a, "j$")
	pressAll(t, a, "'a") // linewise jump back
	if a.cur.Line != 0 {
		t.Fatalf("'a: line = %d, want 0", a.cur.Line)
	}
	// Now mark an indented line and check ' lands on the first non-blank.
	pressAll(t, a, "ma") // mark (0,0)... use the indented line instead
	pressAll(t, a, "j")
	pressAll(t, a, "mb")
	pressAll(t, a, "`a") // exact jump back to (0,0)
	pressAll(t, a, "'b")
	if a.cur.Col != 4 {
		t.Fatalf("'b: col = %d, want 4 (first non-blank of the indented line)", a.cur.Col)
	}
}

func TestUnsetMarkReports(t *testing.T) {
	a := findApp(t, "hello\r\n")
	pressAll(t, a, "`z")
	if a.cur.Line != 0 || a.cur.Col != 0 {
		t.Fatalf("jump to unset mark moved the cursor")
	}
}

func TestMarkJumpClampsStaleMark(t *testing.T) {
	// A mark set on a line that no longer exists must clamp into the buffer
	// instead of corrupting cursor state.
	a := findApp(t, "one\r\ntwo\r\n")
	pressAll(t, a, "j")
	pressAll(t, a, "mc") // mark on line 1
	a.execCommand("clear")
	if a.emu.ScrollbackLen() != 0 {
		t.Fatalf("clear did not reset the buffer")
	}
	pressAll(t, a, "`c")
	if a.cur.Line < 0 || a.cur.Line > a.emu.ScrollbackLen()+a.emu.Height()-1 {
		t.Fatalf("`c jumped out of the buffer: %v", a.cur)
	}
}
