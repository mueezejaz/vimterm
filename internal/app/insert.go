package app

// textEnd returns the column of the last non-space character on a line, or
// -1 when the line is all spaces.
func textEnd(line []rune) int {
	for i := len(line) - 1; i >= 0; i-- {
		if line[i] != ' ' {
			return i
		}
	}
	return -1
}

// firstNonSpace returns the column of the first non-space character on a
// line, or 0 for an all-space line.
func firstNonSpace(line []rune) int {
	for i, r := range line {
		if r != ' ' {
			return i
		}
	}
	return 0
}

// enterInsertAfter is a: enter insert one column right of the cursor, so
// the next character lands after the one under the cursor.
func (a *App) enterInsertAfter() {
	a.syncCursor()
	line := a.bufferLine(a.cur.Line)
	if a.cur.Col <= textEnd(line) {
		a.cur.Col++
	}
	a.enterInsert()
}

// enterInsertEnd is A: enter insert at the end of the line.
func (a *App) enterInsertEnd() {
	a.syncCursor()
	line := a.bufferLine(a.cur.Line)
	a.cur.Col = textEnd(line) + 1
	a.enterInsert()
}

// enterInsertHome is I: enter insert at the first non-space character.
func (a *App) enterInsertHome() {
	a.syncCursor()
	a.cur.Col = firstNonSpace(a.bufferLine(a.cur.Line))
	a.enterInsert()
}