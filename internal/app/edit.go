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
		if text == "" {
			continue
		}
		if sb.Len() > 0 {
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
	var segs []delSeg
	line, at := a.cur.Line, a.cur.Col
	for deleted := 0; deleted < n; {
		text := a.bufferLine(line)
		if text == nil {
			break
		}
		from, to := at, at
		if dir > 0 {
			to = wordStart(text, at, 1, wordKindWord)
			if to < 0 {
				to = textEnd(text) + 1
			}
		} else {
			from = wordStart(text, at, -1, wordKindWord)
			if from < 0 {
				break
			}
		}
		if to <= from {
			if dir > 0 {
				line++
				at = 0
				continue
			}
			break
		}
		if line != a.cur.Line && sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(string(text[from:to]))
		a.emu.DeleteLineCells(line, from, to-from)
		segs = append(segs, delSeg{line, from, to})
		at = from
		deleted++
	}
	a.cur.Line, a.cur.Col = line, at
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

// delSeg is one contiguous deleted range within a single buffer line.
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
	// segment's start and spans every cell that was removed.
	to := from + deleted
	if to > cx {
		return
	}
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