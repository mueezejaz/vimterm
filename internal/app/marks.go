package app

import (
	"vimterm/internal/selection"
)

// markSet holds named buffer positions (m{char} to set, `{char}/'{char} to
// jump). Marks are absolute buffer lines like search matches, so they drift
// once the scrollback saturates and its window slides; jumps clamp into the
// current buffer, which bounds the damage to landing on a shifted line.
// Marks are App-level state shared by all tabs.
type markSet struct {
	m map[rune]selection.Pos
}

func (m *markSet) set(r rune, p selection.Pos) {
	if m.m == nil {
		m.m = make(map[rune]selection.Pos)
	}
	m.m[r] = p
}

func (m *markSet) get(r rune) (selection.Pos, bool) {
	p, ok := m.m[r]
	return p, ok
}

// setMark records the virtual cursor's position under the given mark name.
func (a *App) setMark(r rune) {
	a.syncCursor()
	a.marks.set(r, a.cur)
}

// jumpMark moves the virtual cursor to a mark: lineWise jumps to the first
// non-blank character of the mark's line ('), otherwise to the exact cell
// (`). Stale marks (the buffer moved under them) clamp into the buffer.
func (a *App) jumpMark(r rune, lineWise bool) {
	p, ok := a.marks.get(r)
	if !ok {
		a.setStatusMsg("mark " + string(r) + " not set")
		a.dirty.Store(true)
		return
	}
	if !a.curValid {
		a.syncCursor()
	}
	a.cur = p
	a.clampCursor()
	if lineWise {
		row := rowOf(a.bufferLineCells(a.cur.Line))
		a.cur.Col = row.colAt(firstNonSpace(row.runes))
	}
	a.ensureCursorVisible()
	a.afterCursorMove()
}
