package app

import (
	"fmt"

	"vimterm/internal/clipboard"
	"vimterm/internal/console"
	"vimterm/internal/keybind"
	"vimterm/internal/mode"
	"vimterm/internal/selection"
)

// actionMap binds every known action name to its behavior.
func (a *App) actionMap() map[keybind.Action]func() {
	return map[keybind.Action]func(){
		keybind.ActionMoveLeft:      func() { a.moveCursor(0, -1) },
		keybind.ActionMoveRight:     func() { a.moveCursor(0, 1) },
		keybind.ActionMoveUp:        func() { a.moveCursor(-1, 0) },
		keybind.ActionMoveDown:      func() { a.moveCursor(1, 0) },
		keybind.ActionScrollUp:      func() { a.pageScroll(-1) },
		keybind.ActionScrollDown:    func() { a.pageScroll(1) },
		keybind.ActionGotoTop:       func() { a.jumpCursorTop() },
		keybind.ActionGotoBottom:    func() { a.jumpCursorBottom() },
		keybind.ActionEnterInsert:   a.enterInsert,
		keybind.ActionEnterNormal:   a.enterNormal,
		keybind.ActionSearchForward: a.openSearch,
		keybind.ActionSearchNext:    func() { a.nextSearch(1) },
		keybind.ActionSearchPrev:    func() { a.nextSearch(-1) },
		keybind.ActionCommandPrompt: func() { a.openCommand() },
		keybind.ActionEnterVisual:   a.enterVisual,
		keybind.ActionEnterVisLine:  a.enterVisualLine,
		keybind.ActionCancelVisual:  a.cancelVisual,
		keybind.ActionYank:          a.yank,
		keybind.ActionRecordMacro:   a.recordMacro,
		keybind.ActionPlayMacro:     a.playMacro,
		keybind.ActionRepeatLast:    a.repeatLast,
		keybind.ActionFindChar:      func() { a.find.begin(1, false) },
		keybind.ActionFindCharBack:  func() { a.find.begin(-1, false) },
		keybind.ActionFindUntil:     func() { a.find.begin(1, true) },
		keybind.ActionFindUntilBack: func() { a.find.begin(-1, true) },
		keybind.ActionFindNext:      func() { a.findRepeat(1) },
		keybind.ActionFindPrev:      func() { a.findRepeat(-1) },
		keybind.ActionQuit:          a.requestQuit,
	}
}

// recordMacro handles the q key: start or stop recording.
func (a *App) recordMacro() {
	if a.macro.Replaying() {
		return
	}
	if a.macro.IsRecording() {
		a.macro.StopTruncate()
		a.setStatusMsg("macro recording stopped")
		a.dirty.Store(true)
		return
	}
	a.macro.StartPending(false)
	a.setStatusMsg("awaiting register")
	a.dirty.Store(true)
}

// playMacro handles the @ key: arm playback of a register. Recording is
// stopped first, dropping the terminating @.
func (a *App) playMacro() {
	if a.macro.Replaying() {
		return
	}
	if a.macro.IsRecording() {
		a.macro.StopTruncate()
	}
	a.macro.StartPending(true)
	a.setStatusMsg("awaiting register")
	a.dirty.Store(true)
}

// repeatLast handles the . key: replay the last repeatable unit.
func (a *App) repeatLast() {
	u := a.tracker.unit()
	switch u.kind {
	case unitKeys:
		a.replayKeys(u.keys)
	case unitShell:
		for _, k := range u.keys {
			if bytes := console.KeyToBytes(k); len(bytes) > 0 {
				if _, err := a.sess.Write(bytes); err != nil {
					a.setStatusMsg("write error: " + err.Error())
					return
				}
			}
		}
	default:
		a.setStatusMsg("nothing to repeat")
	}
}

// replayKeys replays a key sequence through the normal key path. Nested
// replays (e.g. a custom command containing ".") are ignored.
func (a *App) replayKeys(seq []keybind.Key) {
	if a.repeating {
		return
	}
	a.repeating = true
	defer func() { a.repeating = false }()
	for _, k := range seq {
		a.handleKey(k)
	}
}

// syncCursor materializes the virtual cursor from the shell cursor (or the
// viewport top when scrolled up) the first time it is needed after entering
// normal/visual mode.
func (a *App) syncCursor() {
	if a.curValid {
		return
	}
	a.curValid = true
	if a.vp.Offset() == 0 {
		cx, cy := a.emu.Cursor()
		a.cur = selection.Pos{Line: a.emu.ScrollbackLen() + cy, Col: cx}
	} else {
		a.cur = selection.Pos{Line: a.topAbsLine(), Col: 0}
	}
	a.clampCursor()
}

// clampCursor keeps the virtual cursor inside the buffer.
func (a *App) clampCursor() {
	if a.cur.Line < 0 {
		a.cur.Line = 0
	}
	if max := a.emu.ScrollbackLen() + a.emu.Height() - 1; a.cur.Line > max {
		a.cur.Line = max
	}
	if a.cur.Col < 0 {
		a.cur.Col = 0
	}
	if max := a.emu.Width() - 1; a.cur.Col > max {
		a.cur.Col = max
	}
}

// moveCursor moves the virtual cursor and scrolls the viewport to keep it
// visible. In visual modes the selection follows.
func (a *App) moveCursor(dl, dc int) {
	a.syncCursor()
	a.cur.Line += dl
	a.cur.Col += dc
	a.clampCursor()
	a.ensureCursorVisible()
	a.afterCursorMove()
}

// pageScroll moves the cursor half a screen like Ctrl+U/D in Vim.
func (a *App) pageScroll(dl int) {
	a.syncCursor()
	step := a.emu.Height() / 2
	if step < 1 {
		step = 1
	}
	a.cur.Line += dl * step
	a.clampCursor()
	a.ensureCursorVisible()
	a.afterCursorMove()
}

// jumpCursorTop jumps to the oldest line (gg).
func (a *App) jumpCursorTop() {
	a.syncCursor()
	a.cur.Line = 0
	a.cur.Col = 0
	a.vp.SetMax(a.emu.ScrollbackLen())
	a.vp.GotoTop()
	a.afterCursorMove()
}

// jumpCursorBottom jumps to the live screen bottom (G).
func (a *App) jumpCursorBottom() {
	a.syncCursor()
	a.cur.Line = a.emu.ScrollbackLen() + a.emu.Height() - 1
	a.cur.Col = 0
	a.vp.GotoBottom()
	a.afterCursorMove()
}

// ensureCursorVisible scrolls the viewport so the virtual cursor is inside
// the visible rows. The scrollback length is refreshed first so a growing
// buffer cannot clamp the scroll against a stale maximum.
func (a *App) ensureCursorVisible() {
	rows := a.emu.Height()
	sbLen := a.emu.ScrollbackLen()
	a.vp.SetMax(sbLen)
	top := a.topAbsLine()
	if a.cur.Line < top {
		a.vp.SetOffset(sbLen - a.cur.Line)
	} else if a.cur.Line > top+rows-1 {
		a.vp.SetOffset(sbLen - (a.cur.Line - rows + 1))
	}
}

// afterCursorMove extends the visual selection when a selection is active.
func (a *App) afterCursorMove() {
	if a.sel.Active {
		a.sel.Move(a.cur)
	}
	a.dirty.Store(true)
}

// enterVisual starts (or toggles) character-wise visual selection.
func (a *App) enterVisual() {
	a.syncCursor()
	switch {
	case !a.sel.Active:
		a.sel.Begin(a.cur)
		a.mods.Enter(mode.ModeVisual)
	case a.mods.Is(mode.ModeVisual):
		a.cancelVisual()
	case a.mods.Is(mode.ModeVisualLine):
		a.sel.SetLineWise(false)
		a.mods.Enter(mode.ModeVisual)
	}
	a.dirty.Store(true)
}

// enterVisualLine starts (or toggles) line-wise visual selection.
func (a *App) enterVisualLine() {
	a.syncCursor()
	switch {
	case !a.sel.Active:
		a.sel.Begin(a.cur)
		a.sel.SetLineWise(true)
		a.mods.Enter(mode.ModeVisualLine)
	case a.mods.Is(mode.ModeVisualLine):
		a.cancelVisual()
	case a.mods.Is(mode.ModeVisual):
		a.sel.SetLineWise(true)
		a.mods.Enter(mode.ModeVisualLine)
	}
	a.dirty.Store(true)
}

// cancelVisual exits visual mode without yanking.
func (a *App) cancelVisual() {
	a.sel.Cancel()
	a.mods.Enter(mode.ModeNormal)
	a.dirty.Store(true)
}

// enterInsert cancels any selection and returns to the shell cursor.
func (a *App) enterInsert() {
	a.sel.Cancel()
	a.vp.GotoBottom()
	a.mods.Enter(mode.ModeInsert)
	a.curValid = false
	a.dirty.Store(true)
}

// enterNormal returns to normal mode, dropping any visual selection. The
// virtual cursor is kept when coming from visual mode; after insert it
// resyncs to the shell cursor.
func (a *App) enterNormal() {
	if a.mods.Current().IsVisual() {
		a.sel.Cancel()
	} else if a.mods.Is(mode.ModeInsert) {
		a.curValid = false
	}
	a.mods.Enter(mode.ModeNormal)
	a.dirty.Store(true)
}

// yank copies the visual selection to the Windows clipboard.
func (a *App) yank() {
	if !a.sel.Active {
		a.setStatusMsg("nothing selected")
		return
	}
	text := a.sel.Text(a.bufferLine)
	lines := 0
	for _, r := range text {
		if r == '\n' {
			lines++
		}
	}
	if text != "" {
		lines++
	}
	if err := clipboard.SetText(text); err != nil {
		a.setStatusMsg("clipboard: " + err.Error())
	} else {
		a.setStatusMsg(fmt.Sprintf("%d lines yanked", lines))
	}
	a.cancelVisual()
}

// openCommand starts a colon-command prompt.
func (a *App) openCommand() {
	a.prompt = newPrompt(promptCommand)
	a.dirty.Store(true)
}