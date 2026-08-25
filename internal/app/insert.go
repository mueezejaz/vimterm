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

// enterInsertAfter is a: enter insert one character right of the cursor, so
// the next character lands after the one under the cursor. A wide character
// is skipped whole, not split in half.
func (a *App) enterInsertAfter() {
	a.syncCursor()
	row := rowOf(a.bufferLineCells(a.cur.Line))
	i := row.runeAt(a.cur.Col)
	if te := textEnd(row.runes); te >= 0 && a.cur.Col <= row.endCol(te) {
		a.cur.Col = row.colAt(i) + row.widths[i]
	}
	a.enterInsert()
}

// enterInsertEnd is A: enter insert at the end of the line.
func (a *App) enterInsertEnd() {
	a.syncCursor()
	row := rowOf(a.bufferLineCells(a.cur.Line))
	a.cur.Col = row.colAt(textEnd(row.runes) + 1)
	a.enterInsert()
}

// enterInsertHome is I: enter insert at the first non-space character.
func (a *App) enterInsertHome() {
	a.syncCursor()
	row := rowOf(a.bufferLineCells(a.cur.Line))
	a.cur.Col = row.colAt(firstNonSpace(row.runes))
	a.enterInsert()
}
