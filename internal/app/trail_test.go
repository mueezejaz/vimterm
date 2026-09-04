package app

import (
	"testing"
	"time"

	"vimterm/internal/emulator"
	"vimterm/internal/mode"
	"vimterm/internal/render"
	"vimterm/internal/selection"
)

// Fixed base time so ghost ages (and therefore opacities) are deterministic.
var trailNow = time.Unix(1700000000, 0)

// trailApp builds a 20x10 app whose 12 content lines leave 3 lines of
// scrollback: the 9 emulator rows show absolute buffer lines 3..11.
func trailApp(t *testing.T) *App {
	t.Helper()
	content := "line zero\r\nline one\r\nline two\r\nline three\r\nline four\r\n" +
		"line five\r\nline six\r\nline seven\r\nline eight\r\nline nine\r\n" +
		"line ten\r\nline eleven"
	a := realApp(t, 20, 10, content)
	a.curValid = true
	a.cur = selection.Pos{Line: 11, Col: 0}
	return a
}

// fillFrame mirrors renderFrame's viewport mapping: frame row y shows
// absolute buffer line sbLen-offset+y.
func fillFrame(a *App, frame *render.Frame, rows, offset int) {
	sbLen := a.emu.ScrollbackLen()
	for y := 0; y < rows; y++ {
		absLine := sbLen - offset + y
		for x := 0; x < frame.Cols; x++ {
			if absLine < sbLen {
				frame.Cells[y][x] = a.emu.ScrollbackCell(x, absLine)
			} else {
				frame.Cells[y][x] = a.emu.Cell(x, absLine-sbLen)
			}
		}
	}
}

func TestLerpColor(t *testing.T) {
	black := emulator.Color{R: 0, G: 0, B: 0}
	white := emulator.Color{R: 255, G: 255, B: 255}
	cases := []struct {
		name string
		a, b emulator.Color
		t    float64
		want emulator.Color
	}{
		{"t=0 returns a", black, white, 0, black},
		{"t=1 returns b", black, white, 1, white},
		{"midpoint", black, white, 0.5, emulator.Color{R: 128, G: 128, B: 128}},
		{"quarter", white, black, 0.25, emulator.Color{R: 191, G: 191, B: 191}},
	}
	for _, tc := range cases {
		if got := lerpColor(tc.a, tc.b, tc.t); got != tc.want {
			t.Errorf("%s: lerpColor = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// withTrailColor sets the configured cursor trail color for the test.
func withTrailColor(a *App, hex string) {
	a.cfgMu.Lock()
	a.cfg.CursorTrail.Color = &hex
	a.cfgMu.Unlock()
}

// TestPaintTrailGhostsFallback covers the no-theme path (plain reverse video)
// plus live-cursor skip, out-of-range ghosts, and expiry.
func TestPaintTrailGhostsFallback(t *testing.T) {
	a := trailApp(t)
	frame := render.NewFrame(a.screenCols, a.screenRows)
	rows := a.emu.Height() // 9 content rows; row 9 is the status line

	// The cursor travels (0,5) → (0,50) → (0,11): ghosts are born at (0,5)
	// and (0,50) when the cursor departs them; (0,11) is the live cursor.
	a.trail.Record(0, 5, trailNow.Add(-150*time.Millisecond))
	a.trail.Record(0, 50, trailNow.Add(-100*time.Millisecond))
	a.trail.Record(0, 11, trailNow.Add(-50*time.Millisecond))

	fillFrame(a, frame, rows, 0)
	a.paintTrailGhosts(frame, rows, trailNow, 0, 11)

	if !frame.Cells[2][0].Reverse {
		t.Fatal("ghost cell at viewport row 2 should be painted with reverse video")
	}
	if frame.Cells[8][0].Reverse {
		t.Fatal("ghost at the live cursor position should be skipped")
	}
	if frame.Cells[0][0].Reverse {
		t.Fatal("ghost on a buffer line above the viewport should not paint row 0")
	}

	// After the trail expires nothing is painted.
	expired := render.NewFrame(a.screenCols, a.screenRows)
	fillFrame(a, expired, rows, 0)
	a.paintTrailGhosts(expired, rows, trailNow.Add(time.Second), 0, 11)
	for y := 0; y < rows; y++ {
		for x := 0; x < expired.Cols; x++ {
			if expired.Cells[y][x].Reverse {
				t.Fatalf("expired ghost left reverse video at row %d col %d", y, x)
			}
		}
	}
}

// TestPaintTrailGhostsBlend covers the theme path: a ghost over a letter
// tints the glyph toward the trail color while keeping it visible, leaving
// the background untouched.
func TestPaintTrailGhostsBlend(t *testing.T) {
	a := trailApp(t)
	a.haveTheme = true
	a.themeFg = emulator.Color{R: 255, G: 255, B: 255}
	a.themeBg = emulator.Color{R: 0, G: 0, B: 0}
	withTrailColor(a, "#FF0000")
	frame := render.NewFrame(a.screenCols, a.screenRows)
	rows := a.emu.Height()

	// The ghost at line 5 departed 50ms ago (below the 64ms sweep dwell, so
	// no interpolated streak): opacity 1-50/300 times the default max
	// opacity 0.6 gives tint strength 0.5.
	a.trail.Record(0, 5, trailNow.Add(-100*time.Millisecond))
	a.trail.Record(0, 11, trailNow.Add(-50*time.Millisecond))

	fillFrame(a, frame, rows, 0)
	a.paintTrailGhosts(frame, rows, trailNow, 0, 11)

	cell := frame.Cells[2][0]
	// White theme fg tinted 50% toward red: R stays 255, G/B halve.
	wantFg := emulator.Color{R: 255, G: 128, B: 128}
	wantBg := emulator.Color{R: 0, G: 0, B: 0, Default: true}
	if cell.Reverse {
		t.Error("tinted ghost cell should not use the reverse attribute")
	}
	if cell.Fg != wantFg || cell.Bg != wantBg {
		t.Errorf("ghost cell = fg %+v bg %+v, want fg %+v bg %+v", cell.Fg, cell.Bg, wantFg, wantBg)
	}
	if cell.Content != "l" {
		t.Errorf("ghost over a letter = %q, want the glyph preserved", cell.Content)
	}
}

// TestPaintTrailGhostsPlainGhost verifies plain departure ghosts share the
// sweep's color language: letters under the trail tint toward the trail color
// and stay readable, blank cells carry the full 8-dot ball.
func TestPaintTrailGhostsPlainGhost(t *testing.T) {
	a := trailApp(t)
	a.haveTheme = true
	a.themeFg = emulator.Color{R: 255, G: 255, B: 255}
	a.themeBg = emulator.Color{R: 0, G: 0, B: 0}
	withTrailColor(a, "#FF0000")
	frame := render.NewFrame(a.screenCols, a.screenRows)
	rows := a.emu.Height()

	// Walk one cell at a time across "line five" (no sweeps: each move is
	// a single cell). Ghosts are born at each departure; the live cursor
	// ends at (5,5).
	steps := []struct {
		col int
		at  time.Duration
	}{
		{0, -time.Second}, // seed
		{1, -100 * time.Millisecond},
		{2, -90 * time.Millisecond},
		{3, -80 * time.Millisecond},
		{4, -70 * time.Millisecond},
		{5, -60 * time.Millisecond},
	}
	for _, s := range steps {
		a.trail.Record(s.col, 5, trailNow.Add(s.at))
	}

	fillFrame(a, frame, rows, 0)
	a.paintTrailGhosts(frame, rows, trailNow, 5, 5)

	// Line 5 sits at viewport row 2. Column 0 holds 'l', column 4 the
	// space. Age 100ms → opacity (1-100/300)*0.6 = 0.4; age 60ms → 0.48.
	if got := frame.Cells[2][0]; got.Content != "l" {
		t.Errorf("ghost over 'l' = %q, want the glyph preserved", got.Content)
	} else if want := (emulator.Color{R: 255, G: 153, B: 153}); got.Fg != want {
		t.Errorf("ghost over 'l' fg = %+v, want %+v", got.Fg, want)
	}
	if got := frame.Cells[2][4]; got.Content != trailBrailleGlyph(0xFF) {
		t.Errorf("ghost over a space = %q, want full ball %q", got.Content, trailBrailleGlyph(0xFF))
	} else if want := (emulator.Color{R: 122, G: 0, B: 0}); got.Fg != want {
		t.Errorf("ghost over a space fg = %+v, want %+v", got.Fg, want)
	}
	if got := frame.Cells[2][1]; got.Content != "i" {
		t.Errorf("ghost over 'i' = %q, want the glyph preserved", got.Content)
	}
}

// TestPaintTrailGhostsSweepOverText verifies the jump comet keeps its dots
// when it crosses text: sweep cells over glyphs stamp their braille pattern
// (full balls shrink to the center cluster) instead of falling back to the
// plain-ghost tint, so word jumps stay visible.
func TestPaintTrailGhostsSweepOverText(t *testing.T) {
	a := trailApp(t)
	a.haveTheme = true
	a.themeFg = emulator.Color{R: 255, G: 255, B: 255}
	a.themeBg = emulator.Color{R: 0, G: 0, B: 0}
	withTrailColor(a, "#FF0000")
	frame := render.NewFrame(a.screenCols, a.screenRows)
	rows := a.emu.Height()

	// Depart line 5 after only 50ms of dwell — below the 64ms stagger
	// threshold, so the sweep paints uniformly at opacity (1-50/300)*0.6
	// = 0.5 (a longer dwell would stagger per-cell birth times).
	a.trail.Record(0, 5, trailNow.Add(-100*time.Millisecond))
	a.trail.Record(15, 5, trailNow.Add(-50*time.Millisecond))

	fillFrame(a, frame, rows, 0)
	a.paintTrailGhosts(frame, rows, trailNow, 15, 5)

	// Line 5 sits at viewport row 2. The band crosses 'i' at column 1 (a
	// glyph, so the full-coverage ball shrinks to the 4-dot cluster) and
	// the space at column 4 (horizontal sweep caps to center 4 dots).
	// Both take the dot foreground lerp(black, red, 0.5), never the tint.
	if got := frame.Cells[2][1]; got.Content != trailBrailleGlyph(0x66) {
		t.Errorf("sweep over 'i' = %q, want the 4-dot cluster %q", got.Content, trailBrailleGlyph(0x66))
	} else if want := (emulator.Color{R: 128, G: 0, B: 0}); got.Fg != want {
		t.Errorf("sweep over 'i' fg = %+v, want %+v", got.Fg, want)
	}
	if got := frame.Cells[2][4]; got.Content != trailBrailleGlyph(0x66) {
		t.Errorf("sweep over a space = %q, want center dots %q", got.Content, trailBrailleGlyph(0x66))
	} else if want := (emulator.Color{R: 128, G: 0, B: 0}); got.Fg != want {
		t.Errorf("sweep over a space fg = %+v, want %+v", got.Fg, want)
	}
}

// TestPaintTrailGhostsPlainWideChar verifies a plain ghost on a wide-character
// lead cell keeps the glyph and fades as inverse video instead of stamping
// braille over the paired glyph.
func TestPaintTrailGhostsPlainWideChar(t *testing.T) {
	a := realApp(t, 20, 10, "日本語テスト\r\nsecond line")
	a.haveTheme = true
	a.themeFg = emulator.Color{R: 255, G: 255, B: 255}
	a.themeBg = emulator.Color{R: 0, G: 0, B: 0}
	a.curValid = true
	a.cur = selection.Pos{Line: 1, Col: 0}
	frame := render.NewFrame(a.screenCols, a.screenRows)
	rows := a.emu.Height()

	a.trail.Record(0, 0, trailNow.Add(-time.Second)) // seed on the lead cell
	a.trail.Record(0, 1, trailNow.Add(-100*time.Millisecond))

	fillFrame(a, frame, rows, 0)
	a.paintTrailGhosts(frame, rows, trailNow, 0, 1)

	cell := frame.Cells[0][0]
	if cell.Content != "日" {
		t.Errorf("wide lead cell content = %q, want the glyph preserved", cell.Content)
	}
	if cell.Reverse {
		t.Error("wide lead cell ghost should blend colors, not use reverse video")
	}
}

// TestPaintTrailGhostsScroll verifies ghosts are painted at the viewport row
// of their absolute buffer line, so scrolling moves the ghost with its text.
func TestPaintTrailGhostsScroll(t *testing.T) {
	a := trailApp(t)
	rows := a.emu.Height()

	a.trail.Record(0, 5, trailNow.Add(-100*time.Millisecond))
	a.trail.Record(0, 11, trailNow.Add(-50*time.Millisecond))

	// Unscrolled: line 5 sits at viewport row 5-3 = 2.
	frame := render.NewFrame(a.screenCols, a.screenRows)
	fillFrame(a, frame, rows, 0)
	a.paintTrailGhosts(frame, rows, trailNow, 0, 11)
	if !frame.Cells[2][0].Reverse {
		t.Fatal("ghost should be at viewport row 2 before scrolling")
	}

	// Scroll up two lines: line 5 moves to viewport row 4.
	a.vp.SetOffset(2)
	scrolled := render.NewFrame(a.screenCols, a.screenRows)
	fillFrame(a, scrolled, rows, 2)
	a.paintTrailGhosts(scrolled, rows, trailNow, 0, 11)
	if !scrolled.Cells[4][0].Reverse {
		t.Fatal("ghost should follow its buffer line to viewport row 4 after scrolling")
	}
	if scrolled.Cells[2][0].Reverse {
		t.Fatal("ghost should not stay at the pre-scroll row")
	}
}

// TestPaintTrailGhostsWideChar verifies ghosts skip wide-character
// continuation cells (the lead cell carries the glyph).
func TestPaintTrailGhostsWideChar(t *testing.T) {
	a := realApp(t, 20, 10, "日本語テスト\r\nsecond line")
	a.curValid = true
	a.cur = selection.Pos{Line: 1, Col: 0}
	frame := render.NewFrame(a.screenCols, a.screenRows)
	rows := a.emu.Height()

	a.trail.Record(1, 0, trailNow.Add(-150*time.Millisecond)) // continuation half
	a.trail.Record(0, 0, trailNow.Add(-100*time.Millisecond)) // lead cell
	a.trail.Record(0, 1, trailNow.Add(-50*time.Millisecond))  // live cursor

	fillFrame(a, frame, rows, 0)
	a.paintTrailGhosts(frame, rows, trailNow, 0, 1)

	if !frame.Cells[0][0].Reverse {
		t.Fatal("ghost on the wide-char lead cell should be painted")
	}
	if frame.Cells[0][1].Reverse {
		t.Fatal("ghost on the wide-char continuation cell should be skipped")
	}
}

// TestPaintTrailGhostsJumpSweep verifies a long jump from a rested position
// paints a gapless streak that grows toward the cursor over the sweep window.
func TestPaintTrailGhostsJumpSweep(t *testing.T) {
	a := trailApp(t)
	rows := a.emu.Height()

	// Rest at line 5, then jump three lines down to the live cursor at
	// line 8. d=3 → sweep points at lines 6 and 7, born 40ms apart
	// (dt = 300*2/(5*3)); viewport rows are line-3.
	a.trail.Record(0, 5, trailNow.Add(-time.Second))
	a.trail.Record(0, 8, trailNow)

	// Right after the jump only the departed origin (row 2) shows.
	frame := render.NewFrame(a.screenCols, a.screenRows)
	fillFrame(a, frame, rows, 0)
	a.paintTrailGhosts(frame, rows, trailNow, 0, 8)
	if !frame.Cells[2][0].Reverse {
		t.Fatal("departed origin should be painted right after the jump")
	}
	if frame.Cells[3][0].Reverse || frame.Cells[4][0].Reverse {
		t.Fatal("sweep points should not be painted before their birth times")
	}

	// At +40ms the first point (line 6 → row 3) joins the streak.
	mid := render.NewFrame(a.screenCols, a.screenRows)
	fillFrame(a, mid, rows, 0)
	a.paintTrailGhosts(mid, rows, trailNow.Add(40*time.Millisecond), 0, 8)
	if !mid.Cells[2][0].Reverse || !mid.Cells[3][0].Reverse {
		t.Fatal("origin and first sweep point should be painted at +40ms")
	}
	if mid.Cells[4][0].Reverse {
		t.Fatal("second sweep point should not be painted before +80ms")
	}

	// At +80ms the streak reaches the cursor: rows 2, 3, 4 all painted.
	end := render.NewFrame(a.screenCols, a.screenRows)
	fillFrame(a, end, rows, 0)
	a.paintTrailGhosts(end, rows, trailNow.Add(80*time.Millisecond), 0, 8)
	for _, y := range []int{2, 3, 4} {
		if !end.Cells[y][0].Reverse {
			t.Fatalf("streak should be painted at row %d at +80ms (gapless)", y)
		}
	}
}

// TestUpdateTrailAltScreen verifies the trail works on the alternate screen
// buffer (nvim, opencode, less): the shell cursor drives it there too.
func TestUpdateTrailAltScreen(t *testing.T) {
	a := realApp(t, 20, 10, "main screen")
	rows := a.emu.Height()

	// Enter the alternate screen and park the shell cursor after "nvim fake"
	// on the top row, like a full-screen app's cursor.
	if _, err := a.emu.Write([]byte("\x1b[?1049h\x1b[2Jnvim fake")); err != nil {
		t.Fatal(err)
	}
	a.altScreen = true // renderFrame mirrors emu.IsAltScreen() on its tick
	a.mods.Enter(mode.ModeInsert)
	a.curValid = false
	frame := render.NewFrame(a.screenCols, a.screenRows)
	fillFrame(a, frame, rows, 0)

	a.updateTrail(frame, rows, trailNow) // seeds the position

	// Move the cursor like an editor jump and render the next frame.
	if _, err := a.emu.Write([]byte("\x1b[3;3H")); err != nil {
		t.Fatal(err)
	}
	a.updateTrail(frame, rows, trailNow.Add(33*time.Millisecond))

	if !frame.Cells[0][9].Reverse {
		t.Fatal("ghost should be painted at the departed alt-screen cell")
	}
	if !frame.Cells[0][8].Reverse {
		t.Fatal("the path between the two positions should be filled (gapless)")
	}
	if frame.Cells[2][2].Reverse {
		t.Fatal("the live cursor cell should not be ghost-painted")
	}
}
