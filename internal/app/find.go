package app

import (
	"fmt"

	"vimterm/internal/keybind"
)

// findState implements the Vim f/F/t/T motion family. f{char} is dynamic
// (the character is chosen at runtime), so it cannot be a static binding:
// the app arms a pending state and consumes the next printable key. The
// armed direction is separate from the last completed find (used by ;/,).
type findState struct {
	dir   int  // last completed direction (+1 forward, -1 backward)
	until bool // last completed until flag (t/T)
	ch    rune
	set   bool // a previous f/t exists (for ; and ,)
	pd    int  // pending direction, 0 = not armed
	pu    bool // pending until flag
}

// begin arms the find for the next character.
func (fs *findState) begin(dir int, until bool) {
	fs.pd = dir
	fs.pu = until
}

// pending reports whether the next printable key selects the target.
func (fs *findState) pending() bool {
	return fs.pd != 0
}

// clear cancels a pending find without touching the stored state.
func (fs *findState) clear() {
	fs.pd = 0
	fs.pu = false
}

// find locates the count-th occurrence of the target on one line and
// returns the cursor column, or -1. The cursor starts at col; f searches
// right of it, F left of it, t stops one cell before the target, T one
// cell after. skipFirst ignores the first match encountered, mirroring
// Vim's handling of ";" / "," after a t/T motion: the cursor sits one
// cell short of the previous target, so a blind repeat re-matches it.
func findOnLine(line []rune, col int, dir int, until bool, ch rune, count int, skipFirst bool) int {
	n := count
	if n < 1 {
		n = 1
	}
	if dir > 0 {
		for i := col + 1; i < len(line); i++ {
			if line[i] == ch {
				if skipFirst {
					skipFirst = false
					continue
				}
				n--
				if n == 0 {
					if until {
						return i - 1
					}
					return i
				}
			}
		}
	} else {
		for i := col - 1; i >= 0; i-- {
			if line[i] == ch {
				if skipFirst {
					skipFirst = false
					continue
				}
				n--
				if n == 0 {
					if until {
						return i + 1
					}
					return i
				}
			}
		}
	}
	return -1
}

// run executes the find with the given target and stores it for ;/, .
func (fs *findState) run(dir int, until bool, ch rune) {
	fs.dir = dir
	fs.until = until
	fs.ch = ch
	fs.set = true
	fs.clear()
}

// isTarget reports whether a key can select a find target: a printable
// character, optionally shifted.
func isTarget(k keybind.Key) (rune, bool) {
	if k.Code != keybind.CodeRune || k.Mods&^keybind.ModShift != 0 {
		return 0, false
	}
	return k.Rune, true
}

// doFind runs one f/F/t/T motion with the given direction, until flag and
// target character, and stores it as the last find for ;/, . The stored
// direction is the original command's, so repeats keep a fixed direction.
// A typed count selects the count-th occurrence.
func (a *App) doFind(dir int, until bool, ch rune) {
	a.find.run(dir, until, ch)
	a.executeFind(dir, until, ch, a.takeCount(), false)
}

// executeFind performs the motion without updating the stored state; used
// by ;/, so their direction is always relative to the original f/t.
// skipFirst drops the first match from the scan (only meaningful for t/T
// repeats).
func (a *App) executeFind(dir int, until bool, ch rune, n int, skipFirst bool) {
	a.syncCursor()
	line := a.bufferLine(a.cur.Line)
	col := findOnLine(line, a.cur.Col, dir, until, ch, n, skipFirst)
	if col == -1 {
		a.setStatusMsg(fmt.Sprintf("find: no %q %s", ch, dirName(dir)))
		a.dirty.Store(true)
		return
	}
	a.cur.Col = col
	a.clampCursor()
	a.ensureCursorVisible()
	a.afterCursorMove()
}

// findRepeat repeats the last f/t: ; keeps the original direction, , flips it.
// A repeat of a t/T motion with no count skips the first candidate in the
// scan, because the cursor sits one cell short of the previous target and a
// blind repeat would immediately re-match it (Vim does the same).
func (a *App) findRepeat(flip int) {
	if !a.find.set {
		a.setStatusMsg("no previous find")
		a.dirty.Store(true)
		return
	}
	n := a.takeCount()
	a.executeFind(a.find.dir*flip, a.find.until, a.find.ch, n, a.find.until && n == 1)
}

func dirName(dir int) string {
	if dir < 0 {
		return "backward"
	}
	return "forward"
}