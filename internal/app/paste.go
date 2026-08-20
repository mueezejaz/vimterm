package app

import (
	"strings"
)

// maxPasteBytes caps the total bytes a counted paste (5p) writes at once.
// The count can reach maxCount (100000), and strings.Repeat allocates the
// whole result up front: a large clipboard times a large count would OOM.
const maxPasteBytes = 1 << 20

// paste types the clipboard text into the shell at the virtual cursor.
// p pastes after the cursor, P at it: the shell cursor is nudged there
// first (the same trick as i), then the text is written.
func (a *App) paste(flip int) {
	if a.sess == nil {
		return
	}
	text, err := a.clipRead()
	if err != nil {
		a.setStatusMsg("clipboard error: " + err.Error())
		a.dirty.Store(true)
		return
	}
	if text == "" {
		a.setStatusMsg("clipboard is empty")
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
		line := a.bufferLine(a.cur.Line)
		if a.cur.Col <= textEnd(line) {
			a.cur.Col++
		}
	}
	a.moveShellCursorToVirtual()
	if _, err := a.sess.Write([]byte(strings.Repeat(text, n))); err != nil {
		a.setStatusMsg("write error: " + err.Error())
		return
	}
	a.curValid = false
	a.dirty.Store(true)
}