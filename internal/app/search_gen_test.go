package app

// Regression test for search drift on a saturated scrollback: with the
// buffer line count as the refresh key, n/N stopped finding anything once
// scrollback hit its cap, because the count froze while lines kept
// shifting. The output-byte generation must invalidate instead.

import (
	"fmt"
	"strings"
	"testing"
)

// writeOut feeds bytes through the same path the reader goroutine uses,
// advancing the tab's output generation alongside the emulator.
func writeOut(a *App, s string) {
	_, _ = a.emu.Write([]byte(s))
	a.tabs[a.active].outBytes.Add(int64(len(s)))
}

func TestNextSearchFindsMatchAfterScrollbackSaturates(t *testing.T) {
	const cap5 = 5
	a := realApp(t, 40, 6, "x\r\n")
	a.emu.SetScrollbackSize(cap5)
	// Saturate: more than cap+screen lines, no needle yet.
	for i := 0; i < 20; i++ {
		writeOut(a, fmt.Sprintf("filler %d\r\n", i))
	}
	sbLen := a.emu.ScrollbackLen()
	rows := a.emu.Height()
	if sbLen != cap5 {
		t.Fatalf("scrollback len = %d, want capped at %d", sbLen, cap5)
	}
	// Search before the needle exists: zero matches.
	a.search.SetQuery([]rune("needle"))
	if len(a.search.Matches()) != 0 {
		t.Fatal("needle found before it was written")
	}
	// New output arrives at constant buffer height (scrollback already
	// saturated), so only the byte generation can know things changed.
	writeOut(a, "needle here\r\n")
	a.nextSearch(1)
	var want int
	for l := 0; l < sbLen+rows; l++ {
		if strings.Contains(string(a.bufferLine(l)), "needle") {
			want = l
			break
		}
	}
	if !strings.Contains(string(a.bufferLine(a.cur.Line)), "needle") {
		t.Fatalf("n landed on line %d (%q), want the needle line %d",
			a.cur.Line, string(a.bufferLine(a.cur.Line)), want)
	}
	if a.cur.Line != want {
		t.Fatalf("cursor line = %d, want %d", a.cur.Line, want)
	}
}

// Counted navigation stays cheap: repeated n with no new output must not
// rescan (the generation check that keeps 99999n from going quadratic).
func TestCountedSearchDoesNotRescan(t *testing.T) {
	a := realApp(t, 40, 30, strings.Repeat("hit\r\n", 10))
	tt := a.tabs[a.active]
	gen := int(tt.outBytes.Load())
	a.search.SetQueryGen([]rune("hit"), gen)
	scans := 0
	for i := 0; i < 50; i++ {
		before := gen
		a.nextSearch(1)
		if int(tt.outBytes.Load()) != before {
			t.Fatal("output changed mid-test")
		}
		scans++
	}
	if scans != 50 {
		t.Fatalf("ran %d steps", scans)
	}
}
