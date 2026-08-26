package app

import (
	"fmt"

	"vimterm/internal/console"
	"vimterm/internal/emulator"
	"vimterm/internal/keybind"
	"vimterm/internal/mode"
	"vimterm/internal/selection"
)

// actionMap binds every known action name to its behavior.
func (a *App) actionMap() map[keybind.Action]func() {
	return map[keybind.Action]func(){
		keybind.ActionMoveLeft:         func() { a.moveCursor(0, -a.takeCount()) },
		keybind.ActionMoveRight:        func() { a.moveCursor(0, a.takeCount()) },
		keybind.ActionMoveUp:           func() { a.moveCursor(-a.takeCount(), 0) },
		keybind.ActionMoveDown:         func() { a.moveCursor(a.takeCount(), 0) },
		keybind.ActionScrollUp:         func() { a.countScroll(-1) },
		keybind.ActionScrollDown:       func() { a.countScroll(1) },
		keybind.ActionGotoTop:          func() { a.countGoto(true) },
		keybind.ActionGotoBottom:       func() { a.countGoto(false) },
		keybind.ActionEnterInsert:      a.enterInsert,
		keybind.ActionEnterInsertAfter: a.enterInsertAfter,
		keybind.ActionEnterInsertEnd:   a.enterInsertEnd,
		keybind.ActionEnterInsertHome:  a.enterInsertHome,
		keybind.ActionEnterNormal:      a.enterNormal,
		keybind.ActionSearchForward:    a.openSearch,
		keybind.ActionSearchNext:       func() { a.countSearch(1) },
		keybind.ActionSearchPrev:       func() { a.countSearch(-1) },
		keybind.ActionCommandPrompt:    func() { a.openCommand() },
		keybind.ActionRenamePrompt:     a.openRenamePrompt,
		keybind.ActionEnterVisual:      a.enterVisual,
		keybind.ActionEnterVisLine:     a.enterVisualLine,
		keybind.ActionCancelVisual:     a.cancelVisual,
		keybind.ActionYank:             a.yank,
		keybind.ActionRecordMacro:      a.recordMacro,
		keybind.ActionPlayMacro:        a.playMacro,
		keybind.ActionRepeatLast:       a.repeatLast,
		keybind.ActionFindChar:         func() { a.find.begin(1, false) },
		keybind.ActionFindCharBack:     func() { a.find.begin(-1, false) },
		keybind.ActionFindUntil:        func() { a.find.begin(1, true) },
		keybind.ActionFindUntilBack:    func() { a.find.begin(-1, true) },
		keybind.ActionFindNext:         func() { a.findRepeat(1) },
		keybind.ActionFindPrev:         func() { a.findRepeat(-1) },
		keybind.ActionMoveWord:         func() { a.wordMotion(1, wordKindWord) },
		keybind.ActionMoveWordBack:     func() { a.wordMotion(-1, wordKindWord) },
		keybind.ActionMoveWordEnd:      func() { a.wordEndMotion(wordKindWord) },
		keybind.ActionMoveWORD:         func() { a.wordMotion(1, wordKindWORD) },
		keybind.ActionMoveWORDBack:     func() { a.wordMotion(-1, wordKindWORD) },
		keybind.ActionMoveWORDEnd:      func() { a.wordEndMotion(wordKindWORD) },
		keybind.ActionPaste:            func() { a.paste(1) },
		keybind.ActionPasteBefore:      func() { a.paste(-1) },
		keybind.ActionDeleteWord:       func() { a.deleteWord(1) },
		keybind.ActionDeleteWordBack:   func() { a.deleteWord(-1) },
		keybind.ActionYankLine:         a.yankLine,
		keybind.ActionQuit:             a.requestQuit,
		keybind.ActionNextTab:          func() { a.countTab(1) },
		keybind.ActionPrevTab:          func() { a.countTab(-1) },
		keybind.ActionNewTab:           a.newTab,
		keybind.ActionTabPopup:         a.openTabPopup,
	}
}

// countTab handles gt/gT: with a count it jumps to that 1-based tab,
// otherwise it cycles in the given direction.
func (a *App) countTab(dir int) {
	hasCount := a.cnt > 0
	n := a.takeCount()
	if hasCount {
		a.activateIndex(n)
		return
	}
	a.activateCycle(dir)
}

// countScroll handles Ctrl+U/D: with a count it moves that many lines,
// otherwise it pages half a screen like Vim.
func (a *App) countScroll(dl int) {
	if n := a.takeCount(); n > 1 {
		a.moveCursor(dl*n, 0)
	} else {
		a.pageScroll(dl)
	}
}

// countGoto handles gg/G with a count: Ngg or NG jump to line N; without a
// count gg goes to the top and G to the bottom. takeCount returns 1 both for
// no count and for a literal 1, so check the raw cnt field to tell them apart
// (1G must jump to line 1, not to the bottom).
func (a *App) countGoto(top bool) {
	hasCount := a.cnt > 0
	n := a.takeCount()
	if hasCount {
		a.jumpCursorLine(n - 1)
	} else if top {
		a.jumpCursorTop()
	} else {
		a.jumpCursorBottom()
	}
}

// countSearch repeats the search n times.
func (a *App) countSearch(dir int) {
	n := a.takeCount()
	for i := 0; i < n; i++ {
		a.nextSearch(dir)
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
	sbLen := a.emu.ScrollbackLen()
	if max := sbLen + a.emu.Height() - 1; a.cur.Line > max {
		a.cur.Line = max
	}
	if a.cur.Col < 0 {
		a.cur.Col = 0
	}
	if max := a.emu.Width() - 1; a.cur.Col > max {
		a.cur.Col = max
	}
	// A wide character's continuation cell (Width 0) is not a real cursor
	// position: styling it highlights only the right half of the glyph and
	// the renderer would redraw from mid-glyph. Snap to the lead cell.
	if a.cur.Col == 0 {
		return
	}
	var c emulator.Cell
	switch y := a.cur.Line - sbLen; {
	case y >= 0:
		c = a.emu.Cell(a.cur.Col, y)
	case a.cur.Line >= 0:
		c = a.emu.ScrollbackCell(a.cur.Col, a.cur.Line)
	default:
		return
	}
	if c.Width == 0 {
		a.cur.Col--
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

// jumpCursorLine jumps to a 0-based absolute line (counted gg/G).
func (a *App) jumpCursorLine(line int) {
	a.syncCursor()
	a.cur.Line = line
	a.cur.Col = 0
	a.clampCursor()
	a.ensureCursorVisible()
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

// enterInsert cancels any selection and returns to the shell cursor. The
// shell's line editor cursor is moved to the virtual cursor first, so text
// typed in insert mode lands exactly where the virtual cursor is.
func (a *App) enterInsert() {
	a.sel.Cancel()
	a.moveShellCursorToVirtual()
	a.vp.GotoBottom()
	a.mods.Enter(mode.ModeInsert)
	a.curValid = false
	a.dirty.Store(true)
}

// moveShellCursorToVirtual nudges the shell's line editor cursor to the
// virtual cursor by sending relative arrow-key escapes. Relative moves are
// independent of the prompt prefix, so this works at any prompt width.
func (a *App) moveShellCursorToVirtual() {
	if a.sess == nil || a.vp.Offset() != 0 {
		return
	}
	a.syncCursor()
	cx, cy := a.emu.Cursor()
	if a.emu.ScrollbackLen()+cy != a.cur.Line {
		return
	}
	delta := cx - a.cur.Col
	if delta == 0 {
		return
	}
	if _, err := a.sess.Write(cursorMoveSeq(delta)); err != nil {
		a.setStatusMsg("write error: " + err.Error())
	}
}

// cursorBlockStyle returns the colors the virtual cursor block should use
// over the given cell: the cell's rendered colors (Reverse applied, Default
// resolved to the theme colors) inverted. Inverting the rendered colors
// instead of just setting Reverse keeps the cursor visible when the cell is
// already highlighted (search matches and selections are drawn with Reverse).
// Defaults are resolved before swapping so the default background attribute
// becomes the default background color even when reverse puts it in the
// foreground slot.
func cursorBlockStyle(cell emulator.Cell, themeFg, themeBg emulator.Color) (fg, bg emulator.Color) {
	fg, bg = cell.Fg, cell.Bg
	zero := emulator.Color{}
	if fg == zero || fg.Default {
		fg = themeFg
	}
	if bg == zero || bg.Default {
		bg = themeBg
	}
	if cell.Reverse {
		fg, bg = bg, fg
	}
	return bg, fg
}

// cursorMoveSeq builds the escape sequences that move a line editor cursor
// by delta cells: positive moves left, negative right.
func cursorMoveSeq(delta int) []byte {
	n, arrow := delta, byte('D') // left
	if delta < 0 {
		n, arrow = -delta, 'C' // right
	}
	seq := make([]byte, 0, 3*n)
	for i := 0; i < n; i++ {
		seq = append(seq, '\x1b', '[', arrow)
	}
	return seq
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
	text := a.sel.Text(a.bufferLineRow)
	lines := 0
	for _, r := range text {
		if r == '\n' {
			lines++
		}
	}
	if text != "" {
		lines++
	}
	if err := a.clipWrite(text); err != nil {
		a.setStatusMsg("clipboard: " + err.Error())
	} else {
		a.setStatusMsg(fmt.Sprintf("%d lines yanked", lines))
	}
	a.cancelVisual()
}

// openCommand starts a colon-command prompt.
func (a *App) openCommand() {
	a.preSearchOffset = a.vp.Offset()
	a.prompt = newPrompt(promptCommand)
	a.dirty.Store(true)
}

// openRenamePrompt starts a colon-command prompt prefilled with "rename "
// so the current tab can be named: enter commits, esc abandons. Meant to be
// chained after new_tab in bindings.
func (a *App) openRenamePrompt() {
	a.openCommand()
	for _, r := range "rename " {
		a.prompt.insert(r)
	}
}
