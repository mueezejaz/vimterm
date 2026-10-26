package app

import (
	"fmt"
	"strings"

	"vimterm/internal/selection"
)

// yankLine implements the yy operator: it copies the whole lines under the
// cursor into the registers and the clipboard, trimmed of the terminal's
// width padding.
func (a *App) yankLine() {
	a.yankOpRange(a.linewiseRange(a.takeCount()))
}

// deleteWord implements dw (dir +1) and db (dir -1) with the count from the
// count prefix.
func (a *App) deleteWord(dir int) {
	a.deleteWordCount(dir, wordKindWord, a.takeCount())
}

// deleteWordCount removes n words under or after the cursor (dir +1, dw) or
// before it (dir -1, db) from the buffer, stores the deleted text in the
// registers, and places the cursor at the start of the deleted region. dw
// continues onto the next line when a line runs out of words.
func (a *App) deleteWordCount(dir, kind, n int) {
	a.syncCursor()
	segs, text, end := a.scanWordDelete(dir, kind, n, true)
	a.cur.Line, a.cur.Col = end.Line, end.Col
	a.clampCursor()
	a.ensureCursorVisible()
	a.dirty.Store(true)
	a.propagateDelete(segs)
	if text == "" {
		a.setStatusMsg("no word to delete")
		return
	}
	a.storeDeleted(text)
	a.setStatusMsg(fmt.Sprintf("%d words deleted", n))
}

// scanWordDelete walks n words (kind word/WORD) like dw/db, collecting the
// covered text and the cell segments. With apply set, each segment is
// deleted from the emulator as it is scanned — the walk continues on the
// shifted line, so later segments are already in post-shift coordinates and
// propagateDelete's contiguity rule sees shell-space coordinates. With apply
// clear (yank) the buffer is untouched. The returned position is where the
// cursor belongs: the start of the last deleted segment.
func (a *App) scanWordDelete(dir, kind, n int, apply bool) ([]delSeg, string, selection.Pos) {
	var sb strings.Builder
	// segs are in cell columns: DeleteLineCells and the shell's line editor
	// both work per cell, so a wide character is never split in half.
	var segs []delSeg
	line := a.cur.Line
	at := rowOf(a.bufferLineCells(line)).runeAt(a.cur.Col)
	curCell := a.cur.Col
	prevLine := line
	for deleted := 0; deleted < n; {
		cells := a.bufferLineCells(line)
		if cells == nil {
			break
		}
		row := rowOf(cells)
		from, to := at, at
		if dir > 0 {
			if at < len(row.runes) && wordChar(kind, row.runes[at]) {
				end := at
				for end < len(row.runes) && wordChar(kind, row.runes[end]) {
					end++
				}
				te := textEnd(row.runes)
				to = end
				for to <= te && to < len(row.runes) && (row.runes[to] == ' ' || row.runes[to] == '\t') {
					to++
				}
			} else {
				to = wordStart(row.runes, at, 1, kind)
				if to < 0 {
					to = textEnd(row.runes) + 1
				}
			}
		} else {
			from = wordStart(row.runes, at, -1, kind)
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
		if line != prevLine && sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		prevLine = line
		sb.WriteString(string(row.runes[from:to]))
		if apply {
			a.emu.DeleteLineCells(line, cellFrom, cellTo-cellFrom)
		}
		segs = append(segs, delSeg{line, cellFrom, cellTo})
		at = from
		curCell = cellFrom
		deleted++
	}
	return segs, sb.String(), selection.Pos{Line: line, Col: curCell}
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
	if _, err := a.writeSess(a.sess, seq); err != nil {
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
