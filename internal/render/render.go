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

// NewFrame allocates an empty frame.
func NewFrame(cols, rows int) *Frame {
	cells := make([][]emulator.Cell, rows)
	for y := range cells {
		cells[y] = make([]emulator.Cell, cols)
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
	var sb strings.Builder
	sb.Grow(f.Cols*f.Rows + f.Rows*8 + 32)

	sb.WriteString(escHideCursor)

	for y := 0; y < f.Rows; y++ {
		sb.WriteString("\x1b[")
		sb.WriteString(itoa(y + 1))
		sb.WriteString(";1H")

		row := f.Cells[y]
		var prevStyle style
		styleActive := false
		lastNonEmpty := -1
		for x := 0; x < f.Cols; x++ {
			c := row[x]
			if significant(c) {
				lastNonEmpty = x
			}
		}

		for x := 0; x <= lastNonEmpty; x++ {
			c := row[x]
			if c.Width == 0 {
				// Placeholder cell of a wide character; the glyph was already
				// written by the preceding cell.
				continue
			}
			st := styleOf(c)
			if !styleActive || st != prevStyle {
				sb.WriteString(escReset)
				writeStyle(&sb, st)
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
			styleActive = false
		}
		if lastNonEmpty < f.Cols-1 {
			sb.WriteString(escEraseLine)
		}
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
}

type style struct {
	fg, bg emulator.Color
	attrs  uint8
}

// significant reports whether a cell must be drawn even when its content is
// a blank: styled cells (the virtual cursor, selection, search highlights,
// status line) are visible on empty space and must not be swallowed by the
// trailing erase. The zero value counts as a blank cell.
func significant(c emulator.Cell) bool {
	if c.Content != "" && c.Content != " " {
		return true
	}
	if c.Reverse || c.Bold || c.Faint || c.Italic || c.Blink ||
		c.Conceal || c.Strike || c.Underline {
		return true
	}
	if c.Fg == (emulator.Color{}) && c.Bg == (emulator.Color{}) {
		return false
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