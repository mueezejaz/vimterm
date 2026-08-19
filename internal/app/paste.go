package app

import (
	"strings"
)

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