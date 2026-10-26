package app

import (
	"strings"
)

// maxPasteBytes caps the total bytes a counted paste (5p) writes at once.
// The count can reach maxCount (100000), and strings.Repeat allocates the
// whole result up front: a large clipboard times a large count would OOM.
const maxPasteBytes = 1 << 20

// paste types register or clipboard text into the shell at the virtual
// cursor. p pastes after the cursor, P at it: the shell cursor is nudged
// there first (the same trick as i), then the text is written. The source is
// the register named by a preceding " prefix, the clipboard for "+/"*, the
// unnamed register when one exists, and — for the no-prefix case with an
// empty register file — the clipboard, which is what p has always pasted.
func (a *App) paste(flip int) {
	if a.sess == nil {
		return
	}
	reg := a.opReg
	a.opReg = 0
	text, err, fromClip := a.pasteSource(reg)
	if err != nil {
		a.setStatusMsg("clipboard error: " + err.Error())
		a.dirty.Store(true)
		return
	}
	if text == "" {
		if fromClip {
			a.setStatusMsg("clipboard is empty")
		} else {
			a.setStatusMsg("register is empty")
		}
		a.dirty.Store(true)
		return
	}
	n := a.takeCount()
	// The Windows clipboard stores \r\n; ConPTY expects \r for Enter, so
	// normalize before writing, or the shell sees two keystrokes per line.
	text = strings.ReplaceAll(text, "\r\n", "\r")
	text = strings.ReplaceAll(text, "\n", "\r")
	if n > 1 && len(text) > maxPasteBytes/n {
		n = maxPasteBytes / len(text)
	}
	if n < 1 {
		n = 1
	}
	a.syncCursor()
	if flip > 0 {
		row := rowOf(a.bufferLineCells(a.cur.Line))
		i := row.runeAt(a.cur.Col)
		if te := textEnd(row.runes); te >= 0 && a.cur.Col <= row.endCol(te) {
			a.cur.Col = row.colAt(i) + row.widths[i]
		}
	}
	a.moveShellCursorToVirtual()
	if _, err := a.writeSess(a.sess, []byte(strings.Repeat(text, n))); err != nil {
		a.setStatusMsg("write error: " + err.Error())
		return
	}
	a.curValid = false
	a.dirty.Store(true)
}

// pasteSource resolves the text a paste writes and whether it came from the
// clipboard (for the empty-source message).
func (a *App) pasteSource(reg rune) (string, error, bool) {
	switch {
	case reg == '+' || reg == '*':
		text, err := a.clipRead()
		return text, err, true
	case reg != 0:
		text, _ := a.regs.get(reg)
		return text, nil, false
	case a.regs.hasUnnamed:
		return a.regs.unnamed, nil, false
	default:
		text, err := a.clipRead()
		return text, err, true
	}
}
