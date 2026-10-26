// Package search implements case-insensitive line search over a text buffer
// (scrollback plus current screen) with match tracking and cell highlighting.
package search

import (
	"sort"
	"unicode"

	"vimterm/internal/emulator"
)

// Line returns the cells of one buffer line.
type Line func(absLine int) []emulator.Cell

// Match is one occurrence of the query: the buffer line and the cell column
// where the occurrence starts.
type Match struct {
	Line int
	Col  int
}

// Search tracks a query and its matches in a fixed snapshot of the buffer.
// The buffer contents are re-read via Line on every Query change and on
// Next/Prev, so callers should pass a Line closure bound to live state.
type Search struct {
	line    Line
	query   []rune
	matches []Match
	gen     int // output generation covered by matches (-1 = none/stale)

	// reusable buffers to avoid per-call allocations
	lowQuery []rune
	lineBuf  []rune
	lowerBuf []rune

	// scanRunes/scanCols flatten one line during recompute; cellStart is
	// Highlight's rune-index-of-cell-start table. All reused across calls.
	scanRunes []rune
	scanCols  []int
	cellStart []int
}

// New creates a Search over the given line provider.
func New(line Line) *Search {
	return &Search{line: line, gen: -1}
}

// Query returns the active query runes.
func (s *Search) Query() []rune {
	return s.query
}

// Clear resets the query and all matches.
func (s *Search) Clear() {
	s.query = nil
	s.matches = nil
	s.gen = -1
}

// SetQuery replaces the query and recomputes all matches over the buffer.
// The next Refresh rescans unconditionally (the generation it would key on
// is unknown).
func (s *Search) SetQuery(q []rune) {
	s.SetQueryGen(q, -1)
}

// SetQueryGen replaces the query and records the output generation the
// recompute covered, so a following Refresh can skip an unchanged buffer.
func (s *Search) SetQueryGen(q []rune, gen int) {
	s.query = q
	s.recompute(gen)
}

// Refresh recomputes the matches for q unless neither the query nor the
// output generation changed since the last scan. The generation must grow
// monotonically with everything the line provider can see — a byte counter
// fed from the session reader works; a buffer line count does not, because
// it stops changing once scrollback saturates while every stored match line
// keeps drifting. Repeat navigation (n/N with a count) calls this with an
// unchanged query; without the generation check each step would rescan the
// whole buffer, making a counted search quadratic.
func (s *Search) Refresh(q []rune, gen int) {
	if gen == s.gen && equalRunes(q, s.query) {
		return
	}
	s.query = q
	s.recompute(gen)
}

func (s *Search) recompute(gen int) {
	s.matches = s.matches[:0]
	s.gen = gen
	if len(s.query) == 0 || s.line == nil {
		return
	}
	s.lowQuery = lowerInto(s.lowQuery[:0], s.query)
	low := s.lowQuery
	// A new batch can appear while we scan; clamp per line read.
	for l := 0; ; l++ {
		cells := s.line(l)
		if cells == nil {
			break
		}
		// Flatten the cell row into runes, remembering the cell column each
		// rune starts at. Wide characters occupy two cells but contribute one
		// rune, so rune indices do not map 1:1 to columns. Ranging over the
		// string decodes runes without allocating a []rune per cell.
		s.scanRunes = s.scanRunes[:0]
		s.scanCols = s.scanCols[:0]
		col := 0
		for _, c := range cells {
			for _, r := range c.Content {
				s.scanRunes = append(s.scanRunes, r)
				s.scanCols = append(s.scanCols, col)
			}
			col += c.Width
		}
		if len(s.scanRunes) == 0 {
			continue
		}
		s.lowerBuf = lowerInto(s.lowerBuf[:0], s.scanRunes)
		text := s.lowerBuf
		// Collect every occurrence, not just the first: navigation must
		// reach same-line duplicates that Highlight shows.
		for from := 0; ; {
			idx := indexFrom(text, low, from)
			if idx < 0 {
				break
			}
			s.matches = append(s.matches, Match{Line: l, Col: s.scanCols[idx]})
			from = idx + len(low)
		}
	}
}

// Matches returns the query occurrences, ascending by position.
func (s *Search) Matches() []Match {
	return s.matches
}

// Next returns the first match strictly after (fromLine, fromCol), or false.
// A fromCol of -1 means "before any column", so Next(l, -1) returns the first
// match at or after line l. Matches are ascending, so a binary search; a
// counted repeat (99999n) steps once per count and a linear scan made each
// step proportional to the whole match list.
func (s *Search) Next(fromLine, fromCol int) (Match, bool) {
	i := sort.Search(len(s.matches), func(i int) bool {
		m := s.matches[i]
		return m.Line > fromLine || (m.Line == fromLine && m.Col > fromCol)
	})
	if i == len(s.matches) {
		return Match{}, false
	}
	return s.matches[i], true
}

// Prev returns the last match strictly before (fromLine, fromCol), or false.
func (s *Search) Prev(fromLine, fromCol int) (Match, bool) {
	hi := sort.Search(len(s.matches), func(i int) bool {
		m := s.matches[i]
		return m.Line > fromLine || (m.Line == fromLine && m.Col >= fromCol)
	})
	if hi == 0 {
		return Match{}, false
	}
	return s.matches[hi-1], true
}

// Highlight marks every occurrence of the query in the given buffer line. When
// styled is false the matches are marked with the reverse attribute, which
// inherits whatever colors the host terminal uses; when it is true, bg is
// painted as the match background instead, so the match stays legible over
// colored output.
func (s *Search) Highlight(line []emulator.Cell, absLine int, bg emulator.Color, styled bool) {
	if len(s.query) == 0 {
		return
	}
	s.lowQuery = lowerInto(s.lowQuery[:0], s.query)
	low := s.lowQuery

	// Build the line's rune representation and the rune index at which each
	// cell starts, reusing buffers. Ranging over Content decodes runes
	// without allocating a []rune per cell.
	s.lineBuf = s.lineBuf[:0]
	s.cellStart = s.cellStart[:0]
	for _, c := range line {
		s.cellStart = append(s.cellStart, len(s.lineBuf))
		for _, r := range c.Content {
			s.lineBuf = append(s.lineBuf, r)
		}
	}
	if len(s.lineBuf) == 0 {
		return
	}
	s.lowerBuf = lowerInto(s.lowerBuf[:0], s.lineBuf)
	text := s.lowerBuf

	// cellEnd(i) is the rune index one past line[i]'s last rune.
	cellEnd := func(i int) int {
		if i+1 < len(s.cellStart) {
			return s.cellStart[i+1]
		}
		return len(s.lineBuf)
	}
	// Matches are found left to right, so the cell cursor only advances:
	// every cell is visited at most once across all matches of the line.
	ci := 0
	for from := 0; ; {
		idx := indexFrom(text, low, from)
		if idx < 0 {
			return
		}
		end := idx + len(low)
		for ci < len(line) && cellEnd(ci) <= idx {
			ci++
		}
		for j := ci; j < len(line) && s.cellStart[j] < end; j++ {
			if styled {
				line[j].Bg = bg
				line[j].Reverse = false
			} else {
				line[j].Reverse = true
			}
		}
		from = end
	}
}

func lowerInto(dst, src []rune) []rune {
	dst = dst[:0]
	for _, x := range src {
		dst = append(dst, unicode.ToLower(x))
	}
	return dst
}

func equalRunes(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// index returns the index of needle in haystack, or -1. Comparison is
// rune-by-rune: converting slices to strings per position (the naive
// `string(a) == string(b)`) allocated twice per candidate on every scanned
// line, which dominated an incremental-search rescan of a large scrollback.
func index(haystack, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	n := len(needle)
	for i := 0; i+n <= len(haystack); i++ {
		if equalRunes(haystack[i:i+n], needle) {
			return i
		}
	}
	return -1
}

// indexFrom returns the index of needle in haystack at or after from, or -1.
func indexFrom(haystack, needle []rune, from int) int {
	rest := haystack[from:]
	if idx := index(rest, needle); idx >= 0 {
		return from + idx
	}
	return -1
}
