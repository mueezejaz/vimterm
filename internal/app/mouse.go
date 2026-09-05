package app

import (
	"fmt"
	"os"
	"sync"
	"time"

	"vimterm/internal/console"
	"vimterm/internal/mode"
	"vimterm/internal/selection"
)

var (
	mouseLog     *os.File
	mouseLogOnce sync.Once
)

func mouseDebugLog(format string, args ...any) {
	mouseLogOnce.Do(func() {
		var err error
		mouseLog, err = os.CreateTemp("", "vimterm-mouse-debug-*.log")
		if err != nil {
			return
		}
		fmt.Fprintf(mouseLog, "vimterm mouse debug log started at %s\n", time.Now())
		fmt.Fprintf(mouseLog, "log file: %s\n", mouseLog.Name())
		mouseLog.Sync()
	})
	if mouseLog == nil {
		return
	}
	fmt.Fprintf(mouseLog, format+"\n", args...)
	mouseLog.Sync()
}

// handleMouse routes console mouse events: when the child has enabled VT
// mouse tracking OR is running a full-screen app in the alternate screen
// buffer, events are encoded as SGR sequences and forwarded to the PTY.
// Otherwise clicks position the cursor, drags build a selection, the wheel
// scrolls the viewport, and double clicks select the word under the pointer.
func (a *App) handleMouse(e console.MouseEvent) {
	mouseMode := a.mouseMode.Load()
	altScreen := a.emu.IsAltScreen()
	mouseDebugLog("MOUSE EVENT: btn=%d x=%d y=%d down=%v drag=%v double=%v mods=%d | mouseMode=%d altScreen=%v",
		e.Button, e.X, e.Y, e.Down, e.Drag, e.Double, e.Modifiers, mouseMode, altScreen)

	if mouseMode != 0 || altScreen {
		a.mousePassthrough(e)
		return
	}

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
		if e.Button != console.MouseNone {
			a.mouseDrag(pos)
		}
	case e.Down:
		a.mouseClick(pos)
	default:
		if e.Button == console.MouseNone {
			a.sel.Cancel()
			a.dirty.Store(true)
		}
	}
}

// mousePassthrough encodes a mouse event as an SGR VT sequence and writes
// it to the PTY so the child process receives it.
func (a *App) mousePassthrough(e console.MouseEvent) {
	vt := console.MouseToVT(e)
	if len(vt) == 0 {
		mouseDebugLog("PASSTHROUGH: empty VT sequence, skipping")
		return
	}

	mouseDebugLog("PASSTHROUGH: writing %d bytes: %q (hex: %x)", len(vt), string(vt), vt)
	n, err := a.sess.Write(vt)
	if err != nil {
		mouseDebugLog("PASSTHROUGH: ERROR writing: %v", err)
		a.setStatusMsg("mouse write error: " + err.Error())
	} else {
		mouseDebugLog("PASSTHROUGH: wrote %d bytes successfully", n)
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
	abs := viewportRowToAbsLine(y, bufBottom, a.vp.Offset(), rows)
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

// mouseWordSelect selects the word under the pointer. wordStart/wordEnd
// implement Vim's b/e motions, which skip the word under the cursor when it
// sits on a boundary, so a dedicated contiguous scan is used instead: it
// expands left and right over word cells from the click column.
func (a *App) mouseWordSelect(pos selection.Pos) {
	cells := a.bufferLineCells(pos.Line)
	isWord := func(x int) bool {
		if x < 0 || x >= len(cells) {
			return false
		}
		rs := []rune(cells[x].Content)
		return len(rs) > 0 && wordChar(wordKindWord, rs[0])
	}
	if !isWord(pos.Col) {
		a.mouseClick(pos)
		return
	}
	start := pos.Col
	for isWord(start - 1) {
		start--
	}
	end := pos.Col
	for isWord(end + 1) {
		end++
	}
	a.mods.Enter(mode.ModeNormal)
	a.sel.Begin(selection.Pos{Line: pos.Line, Col: start})
	a.sel.Move(selection.Pos{Line: pos.Line, Col: end})
	a.cur = selection.Pos{Line: pos.Line, Col: start}
	a.curValid = true
	a.dirty.Store(true)
}
