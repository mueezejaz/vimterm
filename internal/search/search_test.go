package search

import (
	"testing"

	"vimterm/internal/emulator"
)

func fakeLine(texts []string) Line {
	return func(l int) []rune {
		if l < 0 || l >= len(texts) {
			return nil
		}
		return []rune(texts[l])
	}
}

func TestSetQueryAndMatches(t *testing.T) {
	s := New(fakeLine([]string{"Hello world", "world cup", "no match", "WORLD end"}))
	s.SetQuery([]rune("world"))
	got := s.Matches()
	want := []Match{{0, 6}, {1, 0}, {3, 0}}
	if len(got) != len(want) {
		t.Fatalf("matches = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("matches = %v, want %v", got, want)
		}
	}
}

func TestEmptyQuery(t *testing.T) {
	s := New(fakeLine([]string{"aaa", "bbb"}))
	s.SetQuery(nil)
	if s.Matches() != nil {
		t.Fatalf("empty query must not match, got %v", s.Matches())
	}
	s.Clear()
	if s.Query() != nil {
		t.Fatal("Clear must reset the query")
	}
}

func TestNoMatch(t *testing.T) {
	s := New(fakeLine([]string{"aaa", "bbb"}))
	s.SetQuery([]rune("zzz"))
	if len(s.Matches()) != 0 {
		t.Fatalf("expected no matches, got %v", s.Matches())
	}
}

func TestNextPrev(t *testing.T) {
	s := New(fakeLine([]string{"one", "two", "three", "two", "five"}))
	s.SetQuery([]rune("two"))
	if m, ok := s.Next(-1); !ok || m != (Match{1, 0}) {
		t.Fatalf("Next(-1) = %+v,%v want 1,0 true", m, ok)
	}
	if m, ok := s.Next(1); !ok || m != (Match{3, 0}) {
		t.Fatalf("Next(1) = %+v,%v want 3,0 true", m, ok)
	}
	if _, ok := s.Next(3); ok {
		t.Fatal("Next(3) must be false")
	}
	if m, ok := s.Prev(3); !ok || m != (Match{1, 0}) {
		t.Fatalf("Prev(3) = %+v,%v want 1,0 true", m, ok)
	}
	if _, ok := s.Prev(1); ok {
		t.Fatal("Prev(1) must be false")
	}
}

func TestMatchColumn(t *testing.T) {
	s := New(fakeLine([]string{"  hello there", "say hello", "no match"}))
	s.SetQuery([]rune("hello"))
	got := s.Matches()
	want := []Match{{0, 2}, {1, 4}}
	if len(got) != len(want) {
		t.Fatalf("matches = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("matches = %v, want %v", got, want)
		}
	}
	// Case-insensitive matching keeps the same column.
	s.SetQuery([]rune("HELLO"))
	got = s.Matches()
	if len(got) != len(want) {
		t.Fatalf("upper-case matches = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("upper-case matches = %v, want %v", got, want)
		}
	}
}

func TestHighlight(t *testing.T) {
	row := []emulator.Cell{
		{Content: "H", Width: 1},
		{Content: "e", Width: 1},
		{Content: "l", Width: 1},
		{Content: "l", Width: 1},
		{Content: "o", Width: 1},
		{Content: " ", Width: 1},
		{Content: "L", Width: 1},
		{Content: "L", Width: 1},
	}
	s := New(nil)
	s.SetQuery([]rune("ll"))
	s.Highlight(row, 0)
	if !row[2].Reverse || !row[3].Reverse {
		t.Fatal("expected first occurrence highlighted")
	}
	if !row[6].Reverse || !row[7].Reverse {
		t.Fatal("expected second occurrence (LL) fully highlighted")
	}
}

func TestHighlightWideCells(t *testing.T) {
	// A wide char occupies two cells; the continuation cell has no content.
	row := []emulator.Cell{
		{Content: "你", Width: 2},
		{Content: "", Width: 0},
		{Content: "a", Width: 1},
		{Content: "b", Width: 1},
	}
	s := New(nil)
	s.SetQuery([]rune("ab"))
	s.Highlight(row, 0)
	if !row[2].Reverse || !row[3].Reverse {
		t.Fatal("match across continuation cell must highlight the real cells")
	}
	if row[0].Reverse || row[1].Reverse {
		t.Fatal("wide char cells must not be highlighted")
	}
}

func TestCaseInsensitive(t *testing.T) {
	s := New(fakeLine([]string{"AáBC"}))
	s.SetQuery([]rune("ábc"))
	if len(s.Matches()) != 1 {
		t.Fatalf("expected case-insensitive match, got %v", s.Matches())
	}
}