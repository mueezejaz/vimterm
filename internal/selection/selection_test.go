package selection

import (
	"strings"
	"testing"
)

func fakeLine(texts []string) Line {
	return func(l int) ([]rune, []int) {
		if l < 0 || l >= len(texts) {
			return nil, nil
		}
		runes := []rune(texts[l])
		cols := make([]int, len(runes))
		for i := range cols {
			cols[i] = i
		}
		return runes, cols
	}
}

// fakeWideLine serves one line where the first rune is wide (two cells):
// runes [你 a b] start at cell columns [0 2 3].
func fakeWideLine() Line {
	return func(l int) ([]rune, []int) {
		if l != 0 {
			return nil, nil
		}
		return []rune("你ab"), []int{0, 2, 3}
	}
}

const (
	line0 = "alpha beta"
	line1 = "gamma"
	line2 = ""
	line3 = "delta"
)

func lines() []string {
	return []string{line0, line1, line2, line3}
}

func TestCharWiseSingleLine(t *testing.T) {
	var s Selection
	s.Begin(Pos{0, 0})
	s.Move(Pos{0, 4})
	if got := s.Text(fakeLine(lines())); got != "alpha" {
		t.Fatalf("text = %q, want alpha", got)
	}
}

func TestCharWiseReverse(t *testing.T) {
	var s Selection
	s.Begin(Pos{0, 4})
	s.Move(Pos{0, 0})
	if got := s.Text(fakeLine(lines())); got != "alpha" {
		t.Fatalf("text = %q, want alpha", got)
	}
}

func TestCharWiseMultiLine(t *testing.T) {
	var s Selection
	s.Begin(Pos{0, 6})
	s.Move(Pos{1, 2})
	if got := s.Text(fakeLine(lines())); got != "beta\ngam" {
		t.Fatalf("text = %q, want %q", got, "beta\ngam")
	}
}

func TestLineWise(t *testing.T) {
	var s Selection
	s.Begin(Pos{0, 0})
	s.SetLineWise(true)
	s.Move(Pos{3, 0})
	if got := s.Text(fakeLine(lines())); got != "alpha beta\ngamma\n\ndelta" {
		t.Fatalf("text = %q", got)
	}
}

func TestLineWiseReverse(t *testing.T) {
	var s Selection
	s.Begin(Pos{3, 0})
	s.SetLineWise(true)
	s.Move(Pos{1, 0})
	if got := s.Text(fakeLine(lines())); got != "gamma\n\ndelta" {
		t.Fatalf("text = %q", got)
	}
}

func TestContainsCharWise(t *testing.T) {
	var s Selection
	s.Begin(Pos{1, 1})
	s.Move(Pos{2, 2})
	if !s.Contains(Pos{1, 1}) {
		t.Error("anchor must be contained")
	}
	if s.Contains(Pos{1, 0}) {
		t.Error("before anchor must not be contained")
	}
	if !s.Contains(Pos{2, 2}) {
		t.Error("current must be contained")
	}
	if s.Contains(Pos{2, 3}) {
		t.Error("past current must not be contained")
	}
	if s.Contains(Pos{3, 0}) {
		t.Error("outside lines must not be contained")
	}
	if s.Contains(Pos{0, 9}) {
		t.Error("line above must not be contained")
	}
}

func TestContainsLineWise(t *testing.T) {
	var s Selection
	s.Begin(Pos{1, 0})
	s.SetLineWise(true)
	s.Move(Pos{3, 0})
	if !s.Contains(Pos{2, 5}) {
		t.Error("any column between lines must be contained")
	}
	if s.Contains(Pos{0, 0}) {
		t.Error("line above must not be contained")
	}
}

func TestInactive(t *testing.T) {
	var s Selection
	if s.Text(fakeLine(lines())) != "" {
		t.Fatal("inactive selection must produce no text")
	}
	if s.Contains(Pos{0, 0}) {
		t.Fatal("inactive selection must contain nothing")
	}
}

func TestTextTrailingWhitespace(t *testing.T) {
	var s Selection
	s.Begin(Pos{0, 0})
	s.Move(Pos{0, len("alpha beta") - 1})
	got := s.Text(fakeLine(lines()))
	if !strings.HasPrefix(got, "alpha beta") || strings.TrimSuffix(got, " ") == "" {
		t.Fatalf("unexpected text %q", got)
	}
}

func TestCharWiseWideChars(t *testing.T) {
	var s Selection
	// Columns are cell columns: 1 is the continuation cell of 你, so the
	// selection must resolve to whole runes (你..b), never half of one.
	s.Begin(Pos{0, 1})
	s.Move(Pos{0, 3})
	if got := s.Text(fakeWideLine()); got != "你ab" {
		t.Fatalf("text = %q, want 你ab", got)
	}
}
