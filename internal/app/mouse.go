package app

import (
	"vimterm/internal/console"
	"vimterm/internal/mode"
	"vimterm/internal/selection"
)

// handleMouse routes console mouse events: clicks position the cursor,
// drags build a selection, the wheel scrolls the viewport, and double
// clicks select the word under the pointer.
func (a *App) handleMouse(e console.MouseEvent) {
	switch e.Button {
	case console.MouseWheelUp:
		a.mouseScroll(1)
		return
	case console.MouseWheelDown:
		a.mouseScroll(-1)
		return
	case console.MouseRight, console.MouseMiddle:
		return
	}

	pos := a.mousePos(e.X, e.Y)
	switch {
	case e.Double:
		a.mouseWordSelect(pos)
	case e.Drag:
		a.mouseDrag(pos)
	case e.Down:
		a.mouseClick(pos)
	default:
		a.sel.Cancel()
		a.dirty.Store(true)
	}
}

// mousePos converts a console cell (top-left origin, full screen rows) into
// a buffer position. Row 0 of the viewport shows the oldest visible line.
func (a *App) mousePos(x, y int) selection.Pos {
	a.vp.SetMax(a.emu.ScrollbackLen())
	rows := a.emu.Height()
	if rows < 1 {
		rows = a.screenRows
	}
	bufBottom := a.emu.ScrollbackLen() + a.emu.Height() - 1
	abs := bufBottom - a.vp.Offset() - (rows - 1 - y)
	if abs < 0 {
		abs = 0
	}
	if abs > bufBottom {
		abs = bufBottom
	}
	col := x
	if col < 0 {
		col = 0
	}
	if col > a.emu.Width()-1 {
		col = a.emu.Width() - 1
	}
	return selection.Pos{Line: abs, Col: col}
}

// mouseScroll scrolls the viewport by n lines (wheel up shows older
// output). The cursor is left where it is.
func (a *App) mouseScroll(n int) {
	a.vp.SetMax(a.emu.ScrollbackLen())
	if n > 0 {
		a.vp.MoveUp(n)
	} else {
		a.vp.MoveDown(-n)
	}
	a.dirty.Store(true)
}

// mouseClick enters normal mode and moves the virtual cursor to the click
// position. It remembers the position as the anchor for a following drag.
func (a *App) mouseClick(pos selection.Pos) {
	if a.prompt != nil {
		a.cancelPrompt()
	}
	if !a.mods.Is(mode.ModeNormal) {
		a.mods.Enter(mode.ModeNormal)
	}
	a.sel.Cancel()
	a.mouseAnchor = pos
	a.cur = pos
	a.curValid = true
	a.clampCursor()
	a.ensureCursorVisible()
	a.dirty.Store(true)
}

// mouseDrag extends a selection from the click anchor to the current cell.
func (a *App) mouseDrag(pos selection.Pos) {
	if !a.sel.Active {
		a.sel.Begin(a.mouseAnchor)
	}
	a.sel.Move(pos)
	a.cur = pos
	a.curValid = true
	a.dirty.Store(true)
}

// mouseWordSelect selects the word under the pointer.
func (a *App) mouseWordSelect(pos selection.Pos) {
	line := a.bufferLine(pos.Line)
	start := wordStart(line, pos.Col, -1, wordKindWord)
	end := wordEnd(line, pos.Col, wordKindWord)
	if start == -1 || end == -1 {
		a.mouseClick(pos)
		return
	}
	a.mods.Enter(mode.ModeNormal)
	a.sel.Begin(selection.Pos{Line: pos.Line, Col: start})
	a.sel.Move(selection.Pos{Line: pos.Line, Col: end})
	a.cur = selection.Pos{Line: pos.Line, Col: start}
	a.curValid = true
	a.dirty.Store(true)
}