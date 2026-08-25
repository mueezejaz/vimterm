package app

import "vimterm/internal/emulator"

// runeRow flattens a cell row into runes while recording where every rune
// starts. Wide characters occupy two cells but contribute one rune, so rune
// indices never map 1:1 onto cell columns; motions that scan the flattened
// text must convert their results back before touching cursor state or the
// emulator grid.
type runeRow struct {
	runes  []rune
	cols   []int // cols[i] is the first cell column of runes[i]
	widths []int // widths[i] is the cell width of runes[i]
}

// rowOf flattens the given cell row.
func rowOf(cells []emulator.Cell) runeRow {
	var row runeRow
	col := 0
	for _, c := range cells {
		for _, r := range c.Content {
			row.runes = append(row.runes, r)
			row.cols = append(row.cols, col)
			row.widths = append(row.widths, c.Width)
		}
		// A continuation cell of a wide character has Width 0 and occupies
		// no extra columns beyond its lead cell.
		col += c.Width
	}
	return row
}

// runeAt converts a cell column into the index of the rune whose cell span
// contains it; a continuation cell resolves to its lead rune and a column
// past the end clamps to the last rune.
func (r runeRow) runeAt(col int) int {
	for i := range r.cols {
		if col < r.cols[i]+r.widths[i] {
			return i
		}
	}
	if n := len(r.runes); n > 0 {
		return n - 1
	}
	return 0
}

// colAt converts a rune index into its starting cell column. An index equal
// to len(runes) maps one past the final rune's end; others clamp.
func (r runeRow) colAt(i int) int {
	n := len(r.runes)
	switch {
	case n == 0 || i <= 0:
		return 0
	case i >= n:
		return r.cols[n-1] + r.widths[n-1]
	default:
		return r.cols[i]
	}
}

// endCol returns the last cell column occupied by rune i, or -1.
func (r runeRow) endCol(i int) int {
	if i < 0 || i >= len(r.runes) {
		return -1
	}
	return r.cols[i] + r.widths[i] - 1
}
