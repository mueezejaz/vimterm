// Package search implements case-insensitive line search over a text buffer
// (scrollback plus current screen) with match tracking and cell highlighting.
package search

import (
	"strings"
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
	if gen == s.gen && string(q) == string(s.query) {
		return
	}
	s.query = q
	s.recompute(gen)
}

func (s *Search) recompute(gen int) {
	s.matches = nil
	s.gen = gen
	if len(s.query) == 0 || s.line == nil {
		return
	}
	low := lower(s.query)
	// A new batch can appear while we scan; clamp per line read.
	for l := 0; ; l++ {
		cells := s.line(l)
		if cells == nil {
			break
		}
		// Flatten the cell row into runes, remembering the cell column each
		// rune starts at. Wide characters occupy two cells but contribute one
		// rune, so rune indices do not map 1:1 to columns.
		runes := make([]rune, 0, len(cells))
		runeCol := make([]int, 0, len(cells))
		col := 0
		for _, c := range cells {
			for _, r := range []rune(c.Content) {
				runes = append(runes, r)
				runeCol = append(runeCol, col)
			}
			col += c.Width
		}
		if len(runes) == 0 {
			continue
		}
		text := lower(runes)
		// Collect every occurrence, not just the first: navigation must
		// reach same-line duplicates that Highlight shows.
		for from := 0; ; {
			idx := indexFrom(text, low, from)
			if idx < 0 {
				break
			}
			s.matches = append(s.matches, Match{Line: l, Col: runeCol[idx]})
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
// match at or after line l.
func (s *Search) Next(fromLine, fromCol int) (Match, bool) {
	for _, m := range s.matches {
		if m.Line > fromLine || (m.Line == fromLine && m.Col > fromCol) {
			return m, true
		}
	}
	return Match{}, false
}

// Prev returns the last match strictly before (fromLine, fromCol), or false.
func (s *Search) Prev(fromLine, fromCol int) (Match, bool) {
	for i := len(s.matches) - 1; i >= 0; i-- {
		m := s.matches[i]
		if m.Line < fromLine || (m.Line == fromLine && m.Col < fromCol) {
			return m, true
		}
	}
	return Match{}, false
}

// Highlight marks every occurrence of the query in the given buffer line with
// the reverse attribute.
func (s *Search) Highlight(line []emulator.Cell, absLine int) {
	if len(s.query) == 0 {
		return
	}
	runes := make([]rune, 0, len(line))
	for _, c := range line {
		for _, r := range c.Content {
			runes = append(runes, r)
		}
	}
	low := lower(s.query)
	text := lower(runes)
	for from := 0; ; {
		idx := indexFrom(text, low, from)
		if idx < 0 {
			return
		}
		// Map rune indices back onto cells: cells hold at most one rune each
		// (continuation cells hold none).
		runePos := 0
		for j, c := range line {
			n := len([]rune(c.Content))
			if runePos+n > idx && runePos < idx+len(low) {
				line[j].Reverse = true
			}
			runePos += n
		}
		from = idx + len(low)
	}
}

func lower(r []rune) []rune {
	out := make([]rune, len(r))
	for i, x := range r {
		out[i] = unicode.ToLower(x)
	}
	return out
}

// toLower is kept for callers that need a single rune; it delegates to the
// standard library for correct Unicode case folding.
func toLower(r rune) rune {
	return unicode.ToLower(r)
}

// index returns the index of needle in haystack, or -1.
func index(haystack, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
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

// LineText renders a cell row as plain text, trimming trailing spaces.
func LineText(cells []emulator.Cell) string {
	var sb strings.Builder
	for _, c := range cells {
		sb.WriteString(c.Content)
	}
	return strings.TrimRight(sb.String(), " ")
}