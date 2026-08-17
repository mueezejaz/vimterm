// Package selection models Vim-style visual selection over a text buffer
// (scrollback + live screen) using buffer coordinates: an absolute line
// number and a column.
package selection

import (
	"strings"
)

// Pos is a buffer position: an absolute line number and a column.
type Pos struct {
	Line int
	Col  int
}

// Line returns the runes of one buffer line, or nil when it does not exist.
type Line func(absLine int) []rune

// Selection tracks an anchor and the current cursor for char- or line-wise
// visual selection.
type Selection struct {
	Active  bool
	LineWise bool
	Anchor  Pos
	Current Pos
}

// Begin starts a selection at the given position (char-wise by default).
func (s *Selection) Begin(p Pos) {
	s.Active = true
	s.LineWise = false
	s.Anchor = p
	s.Current = p
}

// SetLineWise toggles between char- and line-wise selection.
func (s *Selection) SetLineWise(wise bool) {
	if s.Active {
		s.LineWise = wise
	}
}

// Move updates the current endpoint.
func (s *Selection) Move(p Pos) {
	s.Current = p
}

// Cancel clears the selection.
func (s *Selection) Cancel() {
	s.Active = false
	s.LineWise = false
}

// Start returns the top-left corner of the selection.
func (s *Selection) Start() Pos {
	if s.Current.Line < s.Anchor.Line ||
		(s.Current.Line == s.Anchor.Line && s.Current.Col < s.Anchor.Col) {
		return s.Current
	}
	return s.Anchor
}

// End returns the bottom-right corner of the selection.
func (s *Selection) End() Pos {
	if s.Current.Line > s.Anchor.Line ||
		(s.Current.Line == s.Anchor.Line && s.Current.Col > s.Anchor.Col) {
		return s.Current
	}
	return s.Anchor
}

// Contains reports whether the buffer position is inside the selection.
func (s *Selection) Contains(p Pos) bool {
	if !s.Active {
		return false
	}
	start, end := s.Start(), s.End()
	if s.LineWise {
		return p.Line >= start.Line && p.Line <= end.Line
	}
	if p.Line < start.Line || p.Line > end.Line {
		return false
	}
	if p.Line == start.Line && p.Col < start.Col {
		return false
	}
	if p.Line == end.Line && p.Col > end.Col {
		return false
	}
	return true
}

// Text extracts the selected lines via the line provider. Char-wise
// selections include the columns between start and end (inclusive); line-wise
// selections take whole lines (including blank ones) and join with newlines.
func (s *Selection) Text(line Line) string {
	if !s.Active {
		return ""
	}
	start, end := s.Start(), s.End()
	var sb strings.Builder
	first := true
	for l := start.Line; l <= end.Line; l++ {
		if !first {
			sb.WriteByte('\n')
		}
		first = false
		runes := line(l)
		if len(runes) == 0 {
			continue
		}
		from, to := 0, len(runes)
		if !s.LineWise {
			if l == start.Line {
				from = start.Col
			}
			if l == end.Line {
				to = end.Col + 1
			}
			if from > to {
				from, to = to, from
			}
		}
		if from < 0 {
			from = 0
		}
		if to > len(runes) {
			to = len(runes)
		}
		sb.WriteString(string(runes[from:to]))
	}
	return sb.String()
}