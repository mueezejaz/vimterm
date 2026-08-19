// Package emulator defines the terminal emulation interface and provides an
// implementation backed by github.com/charmbracelet/x/vt.
package emulator

import (
	"image/color"
	"sync"

	"github.com/charmbracelet/x/vt"
	uv "github.com/charmbracelet/ultraviolet"
)

// Color is an RGB color, or the terminal default.
type Color struct {
	R, G, B uint8
	Default bool
}

// Cell is a single terminal cell.
type Cell struct {
	Content string
	Width   int
	Fg      Color
	Bg      Color
	Bold    bool
	Faint   bool
	Italic  bool
	Blink   bool
	Reverse bool
	Conceal bool
	Strike  bool
	Underline bool
}

// Emulator is the virtual terminal: it consumes the raw escape-sequence stream
// from the child process and exposes a cell grid plus cursor state.
type Emulator interface {
	// Write feeds raw output (VT escape sequences) into the emulator.
	Write(p []byte) (int, error)
	// Resize changes the grid size.
	Resize(cols, rows int)
	// Cell returns the cell at column x, row y (0-based).
	Cell(x, y int) Cell
	// Cursor returns the cursor position in grid coordinates.
	Cursor() (x, y int)
	Width() int
	Height() int
	// IsAltScreen reports whether the child has switched to the alternate
	// screen buffer.
	IsAltScreen() bool
	ScrollbackLen() int
	// ScrollbackCell returns the cell at column x of scrolled-off line y
	// (0 = oldest line). Out-of-range positions return a blank cell.
	ScrollbackCell(x, y int) Cell
	// DeleteLineCells removes n cells at column col of the absolute buffer
	// line (scrollback + live screen), shifting the rest of the line left
	// and blanking the freed tail.
	DeleteLineCells(absLine, col, n int)
	ClearScrollback()
	// SetScrollbackSize sets the maximum number of scrollback lines.
	SetScrollbackSize(maxLines int)
	Close() error
}

// vtEmulator adapts vt.SafeEmulator. The extra mutex is needed because the
// underlying accessors return *uv.Cell pointers after releasing their own
// lock, so dereferencing them must stay under ours.
type vtEmulator struct {
	mu   sync.RWMutex
	term *vt.SafeEmulator
}

// New creates a terminal emulator with the given grid size.
func New(cols, rows int) Emulator {
	return &vtEmulator{term: vt.NewSafeEmulator(cols, rows)}
}

func (e *vtEmulator) Write(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.term.Write(p)
}

func (e *vtEmulator) Resize(cols, rows int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.term.Resize(cols, rows)
}

func (e *vtEmulator) Cell(x, y int) Cell {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return fromUVCell(e.term.CellAt(x, y))
}

func (e *vtEmulator) ScrollbackCell(x, y int) Cell {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return fromUVCell(e.term.ScrollbackCellAt(x, y))
}

// DeleteLineCells removes n cells starting at column col of the absolute
// buffer line. On the live screen it shifts the remaining cells left via
// SetCell; on scrollback lines it mutates the stored uv.Line in place.
// Both are safe because every term access in this package holds e.mu.
func (e *vtEmulator) DeleteLineCells(absLine, col, n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if n <= 0 || col < 0 {
		return
	}
	sbLen := e.term.ScrollbackLen()
	cols := e.term.Width()
	if absLine < sbLen {
		line := e.term.Scrollback().Line(absLine)
		if line == nil || col >= len(line) {
			return
		}
		if col+n > len(line) {
			n = len(line) - col
		}
		copy(line[col:], line[col+n:])
		for x := len(line) - n; x < len(line); x++ {
			line[x] = uv.EmptyCell
		}
		return
	}
	y := absLine - sbLen
	if y < 0 || y >= e.term.Height() || col >= cols {
		return
	}
	if col+n > cols {
		n = cols - col
	}
	blank := uv.EmptyCell
	for x := col; x+n < cols; x++ {
		e.term.SetCell(x, y, e.term.CellAt(x+n, y))
	}
	for x := cols - n; x < cols; x++ {
		e.term.SetCell(x, y, &blank)
	}
}

func fromUVCell(c *uv.Cell) Cell {
	if c == nil {
		return Cell{Content: " ", Width: 1}
	}
	return Cell{
		Content:   c.Content,
		Width:     c.Width,
		Fg:        fromColor(c.Style.Fg),
		Bg:        fromColor(c.Style.Bg),
		Bold:      c.Style.Attrs&uv.AttrBold != 0,
		Faint:     c.Style.Attrs&uv.AttrFaint != 0,
		Italic:    c.Style.Attrs&uv.AttrItalic != 0,
		Blink:     c.Style.Attrs&uv.AttrBlink != 0,
		Reverse:   c.Style.Attrs&uv.AttrReverse != 0,
		Conceal:   c.Style.Attrs&uv.AttrConceal != 0,
		Strike:    c.Style.Attrs&uv.AttrStrikethrough != 0,
		Underline: c.Style.Underline != uv.UnderlineNone,
	}
}

func (e *vtEmulator) Cursor() (int, int) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p := e.term.CursorPosition()
	return p.X, p.Y
}

func (e *vtEmulator) Width() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.term.Width()
}

func (e *vtEmulator) Height() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.term.Height()
}

func (e *vtEmulator) IsAltScreen() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.term.IsAltScreen()
}

func (e *vtEmulator) ScrollbackLen() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.term.ScrollbackLen()
}

func (e *vtEmulator) ClearScrollback() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.term.ClearScrollback()
}

func (e *vtEmulator) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.term.Close()
}

func (e *vtEmulator) SetScrollbackSize(maxLines int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.term.SetScrollbackSize(maxLines)
}

// fromColor converts a color into our renderer's representation. Colors that
// are nil or transparent map to the terminal default.
func fromColor(c color.Color) Color {
	if c == nil {
		return Color{Default: true}
	}
	r, g, b, a := c.RGBA()
	if a == 0 {
		return Color{Default: true}
	}
	return Color{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8)}
}