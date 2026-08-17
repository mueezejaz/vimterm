package app

import "testing"

func TestCursorMoveSeq(t *testing.T) {
	cases := []struct {
		delta int
		want  string
	}{
		{0, ""},
		{1, "\x1b[D"},
		{3, "\x1b[D\x1b[D\x1b[D"},
		{-1, "\x1b[C"},
		{-2, "\x1b[C\x1b[C"},
	}
	for _, c := range cases {
		if got := string(cursorMoveSeq(c.delta)); got != c.want {
			t.Errorf("cursorMoveSeq(%d) = %q, want %q", c.delta, got, c.want)
		}
	}
}