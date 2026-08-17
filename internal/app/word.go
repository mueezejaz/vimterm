package app

// Word motion kinds: kind 0 is a Vim "word" (letters, digits, underscore),
// kind 1 is a "WORD" (everything except whitespace).
const (
	wordKindWord = 0
	wordKindWORD = 1
)

// wordChar reports whether r belongs to a word of the given kind.
func wordChar(kind int, r rune) bool {
	if r == ' ' || r == '\t' {
		return false
	}
	if kind == wordKindWORD {
		return true
	}
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
		r >= '0' && r <= '9'
}

// wordStart searches from col in dir for the start of a word. Forward (w)
// skips the word containing col; backward (b) lands on the start of the
// word at or before col. Returns -1 when no word exists on the line.
func wordStart(line []rune, col, dir, kind int) int {
	if col >= len(line) {
		col = len(line) - 1
	}
	if dir > 0 {
		in := col >= 0 && wordChar(kind, line[col])
		for i := col; i < len(line); i++ {
			if c := wordChar(kind, line[i]); c {
				if !in {
					return i
				}
			} else {
				in = false
			}
		}
		return -1
	}
	// Backward: skip a word start under the cursor, then find the previous
	// word start (or the current word's start when inside a word).
	start := col
	if col >= 0 && wordChar(kind, line[col]) &&
		(col == 0 || !wordChar(kind, line[col-1])) {
		start = col - 1
	}
	for i := start; i >= 0; i-- {
		if wordChar(kind, line[i]) && (i == 0 || !wordChar(kind, line[i-1])) {
			return i
		}
	}
	return -1
}

// wordEnd searches from col forward for the end of a word. Already sitting
// on a word's end it moves to the next one (Vim's e). Returns -1 when no
// word remains on the line.
func wordEnd(line []rune, col, kind int) int {
	if col >= len(line) {
		col = len(line) - 1
	}
	start := col
	if col >= 0 && wordChar(kind, line[col]) &&
		(col+1 == len(line) || !wordChar(kind, line[col+1])) {
		start = col + 1
	}
	for i := start; i < len(line); i++ {
		if wordChar(kind, line[i]) && (i+1 == len(line) || !wordChar(kind, line[i+1])) {
			return i
		}
	}
	return -1
}

// wordMotion is the w/b family: moves to word starts. dir +1 is w, -1 is b.
func (a *App) wordMotion(dir, kind int) {
	n := a.takeCount()
	a.syncCursor()
	for c := 0; c < n; c++ {
		a.stepWord(dir, kind)
	}
	a.ensureCursorVisible()
	a.afterCursorMove()
}

// wordEndMotion is the e family: moves to word ends.
func (a *App) wordEndMotion(kind int) {
	n := a.takeCount()
	a.syncCursor()
	for c := 0; c < n; c++ {
		a.stepWordEnd(kind)
	}
	a.ensureCursorVisible()
	a.afterCursorMove()
}

// stepWord advances one word start, crossing line boundaries.
func (a *App) stepWord(dir, kind int) {
	line := a.cur.Line
	for {
		text := a.bufferLine(line)
		from := a.cur.Col
		if line != a.cur.Line {
			if dir > 0 {
				from = 0
			} else {
				from = len(text) - 1
			}
		}
		col := wordStart(text, from, dir, kind)
		if col >= 0 {
			a.cur.Line = line
			a.cur.Col = col
			return
		}
		line += dir
		if line < 0 || line > a.emu.ScrollbackLen()+a.emu.Height()-1 {
			return
		}
	}
}

// stepWordEnd advances one word end, crossing line boundaries.
func (a *App) stepWordEnd(kind int) {
	line := a.cur.Line
	for {
		text := a.bufferLine(line)
		from := a.cur.Col
		if line != a.cur.Line {
			from = 0
		}
		col := wordEnd(text, from, kind)
		if col >= 0 {
			a.cur.Line = line
			a.cur.Col = col
			return
		}
		line++
		if line > a.emu.ScrollbackLen()+a.emu.Height()-1 {
			return
		}
	}
}