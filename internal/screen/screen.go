// Package screen manages the scrollback viewport: which slice of the
// terminal's buffer (scrollback + live screen) is currently visible.
package screen

// Viewport tracks the vertical scroll offset into the scrollback.
//
// offset 0 means the view is pinned to the live screen bottom; larger offsets
// scroll further up into the scrollback.
type Viewport struct {
	rows   int
	offset int
	max    int
}

// New creates a viewport with the given number of visible rows.
func New(rows int) *Viewport {
	if rows < 1 {
		rows = 1
	}
	return &Viewport{rows: rows}
}

// SetRows updates the number of visible rows.
func (v *Viewport) SetRows(rows int) {
	if rows < 1 {
		rows = 1
	}
	v.rows = rows
	v.clamp()
}

// SetMax sets the maximum scroll offset (the scrollback length) and clamps
// the current offset into range. Call it whenever the scrollback changes.
func (v *Viewport) SetMax(max int) {
	if max < 0 {
		max = 0
	}
	v.max = max
	v.clamp()
}

// Offset returns the current scroll offset in lines.
func (v *Viewport) Offset() int {
	return v.offset
}

// ScrolledUp reports whether the view is above the live screen.
func (v *Viewport) ScrolledUp() bool {
	return v.offset > 0
}

// MoveUp scrolls the view up (into the scrollback) by n lines.
func (v *Viewport) MoveUp(n int) {
	if n < 1 {
		return
	}
	v.offset += n
	v.clamp()
}

// MoveDown scrolls the view down (toward the live screen) by n lines.
func (v *Viewport) MoveDown(n int) {
	if n < 1 {
		return
	}
	v.offset -= n
	v.clamp()
}

// PageUp scrolls up by half a screen, like Ctrl+U in Vim.
func (v *Viewport) PageUp() {
	v.MoveUp(v.rows / 2)
}

// PageDown scrolls down by half a screen, like Ctrl+D in Vim.
func (v *Viewport) PageDown() {
	v.MoveDown(v.rows / 2)
}

// GotoTop jumps to the oldest scrollback line.
func (v *Viewport) GotoTop() {
	v.offset = v.max
}

// GotoBottom returns to the live screen.
func (v *Viewport) GotoBottom() {
	v.offset = 0
}

// SetOffset jumps directly to a scroll offset, clamped into range.
func (v *Viewport) SetOffset(n int) {
	v.offset = n
	v.clamp()
}

func (v *Viewport) clamp() {
	if v.offset < 0 {
		v.offset = 0
	}
	if v.offset > v.max {
		v.offset = v.max
	}
}