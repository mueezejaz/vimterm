package app

import (
	"fmt"
	"strings"
)

// yankLine implements yy: it copies the whole line under the cursor to the
// clipboard, trimmed of the terminal's width padding. A count yanks that
// many lines, joined with newlines.
func (a *App) yankLine() {
	n := a.takeCount()
	a.syncCursor()
	var sb strings.Builder
	for i := 0; i < n; i++ {
		line := a.bufferLine(a.cur.Line + i)
		if line == nil {
			break
		}
		text := strings.TrimRight(string(line), " ")
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(text)
	}
	if err := a.clipWrite(sb.String()); err != nil {
		a.setStatusMsg("clipboard: " + err.Error())
	} else {
		a.setStatusMsg(fmt.Sprintf("%d lines yanked", n))
	}
	a.dirty.Store(true)
}

// deleteWord implements dw (dir +1) and db (dir -1): it removes the word
// under or after the cursor (dw) or the word before it (db) from the buffer,
// copies the deleted text to the clipboard, and places the cursor at the
// start of the deleted region. A count repeats the deletion; dw continues
// onto the next line when a line runs out of words.
func (a *App) deleteWord(dir int) {
	n := a.takeCount()
	a.syncCursor()
	var sb strings.Builder
	// segs are in cell columns: DeleteLineCells and the shell's line editor
	// both work per cell, so a wide character is never split in half.
	var segs []delSeg
	line := a.cur.Line
	at := rowOf(a.bufferLineCells(line)).runeAt(a.cur.Col)
	curCell := a.cur.Col
	for deleted := 0; deleted < n; {
		cells := a.bufferLineCells(line)
		if cells == nil {
			break
		}
		row := rowOf(cells)
		from, to := at, at
		if dir > 0 {
			to = wordStart(row.runes, at, 1, wordKindWord)
			if to < 0 {
				to = textEnd(row.runes) + 1
			}
		} else {
			from = wordStart(row.runes, at, -1, wordKindWord)
			if from < 0 {
				break
			}
		}
		if to <= from {
			if dir > 0 {
				line++
				at = 0
				curCell = 0
				continue
			}
			break
		}
		cellFrom, cellTo := row.colAt(from), row.colAt(to)
		if line != a.cur.Line && sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(string(row.runes[from:to]))
		a.emu.DeleteLineCells(line, cellFrom, cellTo-cellFrom)
		segs = append(segs, delSeg{line, cellFrom, cellTo})
		at = from
		curCell = cellFrom
		deleted++
		if dir < 0 && from == to {
			break
		}
	}
	a.cur.Line, a.cur.Col = line, curCell
	a.clampCursor()
	a.ensureCursorVisible()
	a.dirty.Store(true)
	a.propagateDelete(segs)
	if sb.Len() == 0 {
		a.setStatusMsg("no word to delete")
		return
	}
	if err := a.clipWrite(sb.String()); err != nil {
		a.setStatusMsg("clipboard: " + err.Error())
		return
	}
	a.setStatusMsg(fmt.Sprintf("%d words deleted", n))
}

// delSeg is one contiguous deleted range within a single buffer line, in
// cell columns.
type delSeg struct {
	line, from, to int
}

// propagateDelete mirrors the local buffer deletion in the shell's line
// editor: it moves the editor cursor to the end of the deleted range and
// presses backspace for each cell, so the shell's own buffer agrees and a
// later redraw (e.g. typing in insert mode) does not restore the text.
// Only ranges on the shell cursor's line are sent; anything else is not the
// shell's editable buffer.
func (a *App) propagateDelete(segs []delSeg) {
	if a.sess == nil || len(segs) == 0 || a.vp.Offset() != 0 {
		return
	}
	cx, cy := a.emu.Cursor()
	shellLine := a.emu.ScrollbackLen() + cy
	from, deleted := -1, 0
	for _, s := range segs {
		if s.line != shellLine {
			continue
		}
		if from < 0 || s.from < from {
			from = s.from
		}
		deleted += s.to - s.from
	}
	if from < 0 {
		return
	}
	// The local buffer shifts left as cells are removed, so the deleted
	// range in shell coordinates is contiguous: it starts at the first
	// segment's start and spans every cell that was removed. The shell
	// cursor may sit left of the range's end (arrow keys in insert mode
	// move the shell-side cursor), in which case cursorMoveSeq emits right
	// arrows to reach it before backspacing it out.
	to := from + deleted
	seq := cursorMoveSeq(cx - to)
	for i := 0; i < deleted; i++ {
		seq = append(seq, 0x7F)
	}
	if _, err := a.sess.Write(seq); err != nil {
		a.setStatusMsg("write error: " + err.Error())
		return
	}
	// The shell processed the arrows and backspaces, so its cursor now sits
	// at `from`. Nudge the emulator's cursor to match before the shell's own
	// redraw arrives: otherwise the next shell-cursor nudge (e.g. enterInsert)
	// would move an already-correct cursor and typed text lands in the wrong
	// column.
	_, _ = a.emu.Write(cursorMoveSeq(cx - from))
}
