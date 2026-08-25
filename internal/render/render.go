// Package render turns a cell grid into VT escape sequences written to the
// host terminal.
package render

import (
	"io"
	"strings"

	"vimterm/internal/emulator"
)

// Frame is a snapshot of the cell grid to draw.
type Frame struct {
	Cols, Rows int
	Cells      [][]emulator.Cell
	CursorX    int
	CursorY    int
	// CursorVisible controls whether the cursor is placed and shown after
	// drawing (false when the view is scrolled into the scrollback).
	CursorVisible bool
}

// NewFrame allocates an empty frame. Every cell starts with the terminal
// default colors rather than the zero value, so an explicitly painted black
// (#000000) stays distinguishable from "no color" downstream.
func NewFrame(cols, rows int) *Frame {
	def := emulator.Color{Default: true}
	cells := make([][]emulator.Cell, rows)
	for y := range cells {
		cells[y] = make([]emulator.Cell, cols)
		for x := range cells[y] {
			cells[y][x].Fg = def
			cells[y][x].Bg = def
		}
	}
	return &Frame{Cols: cols, Rows: rows, Cells: cells, CursorVisible: true}
}

const (
	escHideCursor = "\x1b[?25l"
	escShowCursor = "\x1b[?25h"
	escReset      = "\x1b[0m"
	escEraseLine  = "\x1b[K"
)

// Draw writes the frame to w as VT escape sequences. It performs a full
// redraw of the grid: the cursor is hidden first, each row is written with
// cursor positioning, and the cursor is placed at (CursorX, CursorY).
func Draw(w io.Writer, f *Frame) {
	New().Draw(w, f)
}

// Renderer draws frames incrementally: rows unchanged since the previous
// draw are not re-emitted, so a full-screen application (nvim etc.) only
// pays for the cells it actually changes. A fresh Renderer (or one that
// sees different dimensions) does a full redraw.
type Renderer struct {
	prev    [][]emulator.Cell
	prevCX  int
	prevCY  int
	prevVis bool
}

// New returns a Renderer that has not drawn anything yet.
func New() *Renderer {
	return &Renderer{prevCX: -1, prevCY: -1}
}

// Draw writes the changed rows of f to w. The cursor is hidden first, then
// only rows that differ from the previous frame are emitted (positioned at
// their first changed cell), and the cursor is placed at (CursorX, CursorY)
// when visible.
func (r *Renderer) Draw(w io.Writer, f *Frame) {
	if r.prev != nil && (len(r.prev) != f.Rows || (f.Rows > 0 && len(r.prev[0]) != f.Cols)) {
		// The grid changed shape; everything must be redrawn.
		r.prev = nil
	}

	var sb strings.Builder
	sb.Grow(f.Cols*f.Rows + f.Rows*8 + 32)
	sb.WriteString(escHideCursor)

	for y := 0; y < f.Rows; y++ {
		row := f.Cells[y]
		var old []emulator.Cell
		oldDrawn := r.prev != nil
		if oldDrawn {
			old = r.prev[y]
		}
		from := 0
		if oldDrawn {
			from = firstDiff(row, old)
			if from < 0 {
				continue
			}
		}
		drawRow(&sb, row, old, oldDrawn, y, from, f.Cols)
	}

	if f.CursorVisible {
		// Place the cursor. Cursor coordinates are 1-based; clamp into the grid.
		cx, cy := f.CursorX+1, f.CursorY+1
		if cx < 1 {
			cx = 1
		}
		if cy < 1 {
			cy = 1
		}
		if cx > f.Cols {
			cx = f.Cols
		}
		if cy > f.Rows {
			cy = f.Rows
		}
		sb.WriteString("\x1b[")
		sb.WriteString(itoa(cy))
		sb.WriteByte(';')
		sb.WriteString(itoa(cx))
		sb.WriteByte('H')
		sb.WriteString(escShowCursor)
	}

	_, _ = io.WriteString(w, sb.String())

	r.prevCX, r.prevCY, r.prevVis = f.CursorX, f.CursorY, f.CursorVisible
	if r.prev == nil || len(r.prev) != f.Rows || len(r.prev[0]) != f.Cols {
		prev := make([][]emulator.Cell, f.Rows)
		for y := range prev {
			prev[y] = make([]emulator.Cell, f.Cols)
			for x := range prev[y] {
				prev[y][x].Fg = emulator.Color{Default: true}
				prev[y][x].Bg = emulator.Color{Default: true}
			}
		}
		r.prev = prev
	}
	for y := range f.Cells {
		copy(r.prev[y], f.Cells[y])
	}
}

// drawRow emits one row starting at cell from: position the cursor, write the
// styled cells from `from` through the last significant cell, and erase the
// tail when it previously held content that is gone now.
func drawRow(sb *strings.Builder, row, old []emulator.Cell, oldDrawn bool, y, from, cols int) {
	lastNonEmpty := -1
	for x := from; x < cols; x++ {
		if significant(row[x]) {
			lastNonEmpty = x
		}
	}

	sb.WriteString("\x1b[")
	sb.WriteString(itoa(y + 1))
	sb.WriteByte(';')
	sb.WriteString(itoa(from + 1))
	sb.WriteByte('H')

	var prevStyle style
	styleActive := false
	for x := from; x <= lastNonEmpty; x++ {
		c := row[x]
		if c.Width == 0 {
			// Placeholder cell of a wide character; the glyph was already
			// written by the preceding cell.
			continue
		}
		st := styleOf(c)
		if !styleActive || st != prevStyle {
			sb.WriteString(escReset)
			writeStyle(sb, st)
			prevStyle = st
			styleActive = true
		}
		if c.Content == "" {
			sb.WriteByte(' ')
		} else {
			sb.WriteString(c.Content)
		}
	}

	if styleActive {
		sb.WriteString(escReset)
	}
	if lastNonEmpty < cols-1 && (!oldDrawn || oldTailSignificant(old, lastNonEmpty)) {
		sb.WriteString(escEraseLine)
	}
}

// firstDiff returns the first column where row and old differ, or -1 when
// the rows are identical.
func firstDiff(row, old []emulator.Cell) int {
	for x := range row {
		if row[x] != old[x] {
			return x
		}
	}
	return -1
}

// oldTailSignificant reports whether the previously drawn row had any
// significant cell at or beyond from, so stale content would need erasing.
func oldTailSignificant(old []emulator.Cell, from int) bool {
	for x := from + 1; x < len(old); x++ {
		if significant(old[x]) {
			return true
		}
	}
	return false
}

type style struct {
	fg, bg emulator.Color
	attrs  uint8
}

// significant reports whether a cell must be drawn even when its content is
// a blank: styled cells (the virtual cursor, selection, search highlights,
// status line) are visible on empty space and must not be swallowed by the
// trailing erase. Frames initialize their cells to the terminal default, so
// a zero-value (explicitly painted black) color means styled content here,
// never "no color".
func significant(c emulator.Cell) bool {
	if c.Content != "" && c.Content != " " {
		return true
	}
	if c.Reverse || c.Bold || c.Faint || c.Italic || c.Blink ||
		c.Conceal || c.Strike || c.Underline {
		return true
	}
	return !c.Fg.Default || !c.Bg.Default
}

const (
	attrBold = 1 << iota
	attrFaint
	attrItalic
	attrBlink
	attrReverse
	attrConceal
	attrStrike
	attrUnderline
)

func styleOf(c emulator.Cell) style {
	var s style
	s.fg, s.bg = c.Fg, c.Bg
	if c.Bold {
		s.attrs |= attrBold
	}
	if c.Faint {
		s.attrs |= attrFaint
	}
	if c.Italic {
		s.attrs |= attrItalic
	}
	if c.Blink {
		s.attrs |= attrBlink
	}
	if c.Reverse {
		s.attrs |= attrReverse
	}
	if c.Conceal {
		s.attrs |= attrConceal
	}
	if c.Strike {
		s.attrs |= attrStrike
	}
	if c.Underline {
		s.attrs |= attrUnderline
	}
	return s
}

func writeStyle(sb *strings.Builder, st style) {
	var codes []string
	if st.attrs&attrBold != 0 {
		codes = append(codes, "1")
	}
	if st.attrs&attrFaint != 0 {
		codes = append(codes, "2")
	}
	if st.attrs&attrItalic != 0 {
		codes = append(codes, "3")
	}
	if st.attrs&attrUnderline != 0 {
		codes = append(codes, "4")
	}
	if st.attrs&attrBlink != 0 {
		codes = append(codes, "5")
	}
	if st.attrs&attrReverse != 0 {
		codes = append(codes, "7")
	}
	if st.attrs&attrConceal != 0 {
		codes = append(codes, "8")
	}
	if st.attrs&attrStrike != 0 {
		codes = append(codes, "9")
	}
	if !st.fg.Default {
		codes = append(codes, fgRGB(st.fg))
	}
	if !st.bg.Default {
		codes = append(codes, bgRGB(st.bg))
	}
	if len(codes) == 0 {
		return
	}
	sb.WriteString("\x1b[")
	sb.WriteString(strings.Join(codes, ";"))
	sb.WriteByte('m')
}

func fgRGB(c emulator.Color) string {
	var b strings.Builder
	b.WriteString("38;2;")
	b.WriteString(itoa(int(c.R)))
	b.WriteByte(';')
	b.WriteString(itoa(int(c.G)))
	b.WriteByte(';')
	b.WriteString(itoa(int(c.B)))
	return b.String()
}

func bgRGB(c emulator.Color) string {
	var b strings.Builder
	b.WriteString("48;2;")
	b.WriteString(itoa(int(c.R)))
	b.WriteByte(';')
	b.WriteString(itoa(int(c.G)))
	b.WriteByte(';')
	b.WriteString(itoa(int(c.B)))
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}