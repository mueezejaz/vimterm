package cursortrail

import (
	"math"
	"testing"
	"time"
)

func testTrail() *Trail {
	return New(Config{
		Enabled:      true,
		Duration:     300 * time.Millisecond,
		MaxPositions: 40,
	})
}

func TestTrailBasic(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     300 * time.Millisecond,
		MaxPositions: 12,
	})
	now := time.Now()

	// The first record only seeds the position: nothing has departed yet.
	trail.Record(5, 3, now)
	if got := trail.Ghosts(now); len(got) != 0 {
		t.Fatalf("expected 0 ghosts after first record, got %d", len(got))
	}

	// Moving to (5,4) 50ms later departs (5,3): one ghost, full opacity.
	trail.Record(5, 4, now.Add(50*time.Millisecond))
	ghosts := trail.Ghosts(now.Add(50 * time.Millisecond))
	if len(ghosts) != 1 {
		t.Fatalf("expected 1 ghost, got %d", len(ghosts))
	}
	if ghosts[0].X != 5 || ghosts[0].Line != 3 {
		t.Fatalf("ghost at (x=%d,line=%d), want (5,3)", ghosts[0].X, ghosts[0].Line)
	}

	// A third position departs (5,4): two ghosts, oldest fainter.
	trail.Record(5, 5, now.Add(100*time.Millisecond))
	ghosts = trail.Ghosts(now.Add(100 * time.Millisecond))
	if len(ghosts) != 2 {
		t.Fatalf("expected 2 ghosts, got %d", len(ghosts))
	}
	if ghosts[0].X != 5 || ghosts[0].Line != 3 {
		t.Fatalf("oldest ghost at (x=%d,line=%d), want (5,3)", ghosts[0].X, ghosts[0].Line)
	}
	if ghosts[0].Opacity >= ghosts[1].Opacity {
		t.Fatalf("oldest ghost opacity %v should be lower than newest %v", ghosts[0].Opacity, ghosts[1].Opacity)
	}
}

func TestTrailOldEntriesSkipped(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     100 * time.Millisecond,
		MaxPositions: 12,
	})
	now := time.Now()

	// Positions at T=0,50,100,150 → departures at T=50,100,150.
	trail.Record(1, 0, now)
	trail.Record(2, 0, now.Add(50*time.Millisecond))
	trail.Record(3, 0, now.Add(100*time.Millisecond))
	trail.Record(4, 0, now.Add(150*time.Millisecond))

	// At T=200ms: (1,0) departed at 50 (age 150, expired), (2,0) departed at
	// 100 (age 100, opacity 0, skipped), (3,0) departed at 150 (age 50).
	// Bug was: break on first expired entry skipped ALL remaining entries.
	ghosts := trail.Ghosts(now.Add(200 * time.Millisecond))
	if len(ghosts) != 1 {
		t.Fatalf("expected 1 surviving ghost, got %d", len(ghosts))
	}
	if ghosts[0].X != 3 {
		t.Fatalf("surviving ghost x=%d, want 3", ghosts[0].X)
	}
}

// TestTrailGhostBornAtDeparture is the regression test for moves made after
// sitting still: the ghost is born when the cursor leaves, so even the first
// j/k/h/l after minutes of idle leaves a trail.
func TestTrailGhostBornAtDeparture(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     300 * time.Millisecond,
		MaxPositions: 12,
	})
	t0 := time.Unix(1700000000, 0)

	// Sit at (5,3) far longer than the fade duration, then move one line
	// down (same column — the vertical, j-style case).
	trail.Record(5, 3, t0)
	trail.Record(5, 4, t0.Add(5*time.Second))

	ghosts := trail.Ghosts(t0.Add(5 * time.Second))
	if len(ghosts) != 1 {
		t.Fatalf("expected 1 ghost after a move from rest, got %d", len(ghosts))
	}
	if ghosts[0].X != 5 || ghosts[0].Line != 3 || ghosts[0].Opacity != 1.0 {
		t.Fatalf("ghost = %+v, want (x=5,line=3) opacity 1.0", ghosts[0])
	}
	if !trail.Active(t0.Add(5 * time.Second)) {
		t.Fatal("trail should be active right after a move from rest")
	}
}

func TestTrailDisabled(t *testing.T) {
	trail := New(Config{Enabled: false, Duration: 300 * time.Millisecond, MaxPositions: 12})
	now := time.Now()
	trail.Record(1, 1, now)
	trail.Record(2, 1, now.Add(10*time.Millisecond))
	if got := trail.Ghosts(now.Add(10 * time.Millisecond)); got != nil {
		t.Fatalf("Ghosts on disabled trail = %+v, want nil", got)
	}
	if trail.Active(now.Add(10 * time.Millisecond)) {
		t.Fatal("disabled trail should never be Active")
	}
}

func TestTrailUnchangedPositionNotRecorded(t *testing.T) {
	trail := New(Config{Enabled: true, Duration: 300 * time.Millisecond, MaxPositions: 12})
	now := time.Now()
	trail.Record(2, 2, now)
	trail.Record(2, 2, now.Add(10*time.Millisecond))
	trail.Record(2, 2, now.Add(20*time.Millisecond))
	if got := trail.Ghosts(now.Add(20 * time.Millisecond)); len(got) != 0 {
		t.Fatalf("expected 0 ghosts after repeated same-position records, got %d", len(got))
	}
}

func TestTrailActive(t *testing.T) {
	trail := New(Config{Enabled: true, Duration: 100 * time.Millisecond, MaxPositions: 12})
	now := time.Now()

	if trail.Active(now) {
		t.Fatal("empty trail should not be Active")
	}
	trail.Record(1, 1, now)
	if trail.Active(now.Add(50 * time.Millisecond)) {
		t.Fatal("seeding the first position departs nothing yet")
	}
	trail.Record(2, 1, now.Add(50*time.Millisecond))
	if !trail.Active(now.Add(100 * time.Millisecond)) {
		t.Fatal("trail should be Active 50ms after the departure with 100ms duration")
	}
	if !trail.Active(now.Add(150 * time.Millisecond)) {
		t.Fatal("trail should be Active at the duration boundary")
	}
	if trail.Active(now.Add(155 * time.Millisecond)) {
		t.Fatal("trail should not be Active after the ghost expired")
	}
}

// coveredSet merges the ghosts into a per-cell coverage map (masks OR-ed).
func coveredSet(ghosts []Ghost) map[[2]int]uint8 {
	m := map[[2]int]uint8{}
	for _, g := range ghosts {
		if g.BrailleMask != 0 {
			m[[2]int{g.X, g.Line}] |= g.BrailleMask
		} else {
			m[[2]int{g.X, g.Line}] |= g.Mask
		}
	}
	return m
}

// assertBandConnected verifies the covered cells form one edge-connected
// region running from the origin: no gaps, no corner-only (staircase)
// connections anywhere in the band.
func assertBandConnected(t *testing.T, covered map[[2]int]uint8, from [2]int) {
	t.Helper()
	seen := map[[2]int]bool{from: true}
	queue := [][2]int{from}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		for _, n := range [4][2]int{{c[0] + 1, c[1]}, {c[0] - 1, c[1]}, {c[0], c[1] + 1}, {c[0], c[1] - 1}} {
			if _, ok := covered[n]; ok && !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	if len(seen) != len(covered) {
		t.Fatalf("trail band is disconnected: only %d of %d cells reachable from the origin", len(seen), len(covered))
	}
}

// assertBandGeometry verifies the band hugs the straight segment between the
// two cursor centers: every covered cell's rectangle stays within tol column
// units of the line (measured to its nearest corner, so a legitimate
// quarter-cell edge sliver is not flagged), and every sampled centerline
// point lands in a covered cell (no holes along the path). Rows count double
// so the geometry matches the on-screen cell aspect. origin is owned by the
// departure ghost, not the band. bornThrough bounds the hole check to the
// fraction of the path whose staggered sub-cells are born by the sample time
// (1.0 once the whole band has been born).
func assertBandGeometry(t *testing.T, covered map[[2]int]uint8, origin [2]int, x0, l0, x1, l1 int, tol, bornThrough float64) {
	t.Helper()
	px, py := float64(x0)+0.5, float64(l0)*2+1
	qx, qy := float64(x1)+0.5, float64(l1)*2+1
	dx, dy := qx-px, qy-py
	l := math.Hypot(dx, dy)
	for cell := range covered {
		if cell == origin {
			continue
		}
		best := math.Inf(1)
		for _, cx := range [2]float64{float64(cell[0]), float64(cell[0] + 1)} {
			for _, cy := range [2]float64{float64(cell[1]) * 2, float64(cell[1])*2 + 2} {
				if perp := math.Abs((cx-px)*dy-(cy-py)*dx) / l; perp < best {
					best = perp
				}
			}
		}
		if best > tol {
			t.Fatalf("covered cell (%d,%d) lies %.2f off the band (tolerance %.2f) — trail is not a straight smear",
				cell[0], cell[1], best, tol)
		}
	}
	const steps = 64
	for i := 1; i < steps; i++ {
		s := float64(i) / steps
		if s > bornThrough {
			break
		}
		x, y := px+dx*s, py+dy*s
		cell := [2]int{int(x), int(y / 2)}
		if cell == origin {
			continue
		}
		if _, ok := covered[cell]; !ok {
			t.Fatalf("centerline point (%.2f,%.2f) falls in uncovered cell (%d,%d) — hole in the band", x, y, cell[0], cell[1])
		}
	}
}

// TestTrailJumpFromRestSweepsPath verifies a long jump from a rested position
// sweeps the path as a band that grows toward the cursor.
func TestTrailJumpFromRestSweepsPath(t *testing.T) {
	trail := testTrail()
	t0 := time.Unix(1700000000, 0)

	// Rest, then an 8-cell horizontal jump (word-motion style). The sweep
	// window is 120ms; cell x is born at 120ms * (x-2.5)/8.
	trail.Record(2, 5, t0)
	trail.Record(10, 5, t0.Add(time.Second))

	// Right at the jump only the departed origin exists; the band is unborn.
	ghosts := trail.Ghosts(t0.Add(time.Second))
	if len(ghosts) != 1 || ghosts[0].X != 2 || ghosts[0].Line != 5 {
		t.Fatalf("origin ghost = %+v, want (2,5)", ghosts)
	}

	// 110ms in, cells 3..9 are born; the head cell 10 arrives at 116ms.
	ghosts = trail.Ghosts(t0.Add(time.Second).Add(110 * time.Millisecond))
	if len(ghosts) != 8 {
		t.Fatalf("expected departure + 7 band cells at +110ms, got %d: %+v", len(ghosts), ghosts)
	}
	if ghosts[0].X != 2 || ghosts[0].Mask != 0 {
		t.Fatalf("oldest ghost = %+v, want the departed origin (2,5) full-cell", ghosts[0])
	}
	covered := coveredSet(ghosts)
	for x := 3; x <= 9; x++ {
		if covered[[2]int{x, 5}] != 0xFF {
			t.Fatalf("cell (%d,5) coverage %d, want full 0xFF (band must be gapless)", x, covered[[2]int{x, 5}])
		}
	}
	if _, ok := covered[[2]int{10, 5}]; ok {
		t.Fatal("head cell should not be born before +116ms")
	}
	// The band's head (nearest the cursor) is the most opaque ghost.
	if ghosts[7].Opacity <= ghosts[1].Opacity {
		t.Fatalf("band head opacity %v should exceed tail %v", ghosts[7].Opacity, ghosts[1].Opacity)
	}

	// At +130ms the head cell joins, covering only the half toward the band.
	ghosts = trail.Ghosts(t0.Add(time.Second).Add(130 * time.Millisecond))
	if len(ghosts) != 9 {
		t.Fatalf("expected departure + 8 band cells at +130ms, got %d", len(ghosts))
	}
	if got := coveredSet(ghosts)[[2]int{10, 5}]; got == 0 {
		t.Fatalf("head cell (10,5) coverage %d, want nonzero toward the band", got)
	}
}

// TestTrailVerticalSweepCoversPath verifies a dir-style vertical jump paints
// a gapless streak of full-width cells down the cursor's column.
func TestTrailVerticalSweepCoversPath(t *testing.T) {
	trail := testTrail()
	t0 := time.Unix(1700000000, 0)

	trail.Record(3, 10, t0)
	trail.Record(3, 40, t0.Add(time.Second))

	// 30-line jump: departure + lines 11..39 full-width + line 40 capped at
	// its upper half. All births land inside the 120ms window.
	ghosts := trail.Ghosts(t0.Add(time.Second).Add(120 * time.Millisecond))
	if len(ghosts) != 31 {
		t.Fatalf("expected departure + 30 band cells, got %d: %+v", len(ghosts), ghosts)
	}
	if ghosts[0].Line != 10 {
		t.Fatalf("oldest ghost line=%d, want 10 (origin)", ghosts[0].Line)
	}
	covered := coveredSet(ghosts)
	for l := 11; l < 40; l++ {
		if got := covered[[2]int{3, l}]; got != 0xFF {
			t.Fatalf("cell (3,%d) coverage %d, want full 0xFF (streak must be gapless)", l, got)
		}
	}
	// The head caps at the live cursor's upper half (flat cut at its center).
	if got := covered[[2]int{3, 40}]; got == 0 {
		t.Fatalf("head cell (3,40) coverage %d, want nonzero toward the band", got)
	}
}

// TestTrailLongJumpStaysGapless verifies a jump far longer than the initial
// ring capacity still gets a per-cell band (the buffer grows to fit).
func TestTrailLongJumpStaysGapless(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     time.Second,
		MaxPositions: 40,
	})
	t0 := time.Unix(1700000000, 0)

	// 200-line jump: departure + lines 101..300.
	trail.Record(0, 100, t0)
	trail.Record(0, 300, t0.Add(time.Second))

	ghosts := trail.Ghosts(t0.Add(time.Second).Add(500 * time.Millisecond))
	if len(ghosts) != 201 {
		t.Fatalf("expected departure + 200 band cells, got %d", len(ghosts))
	}
	if ghosts[0].Line != 100 {
		t.Fatalf("oldest ghost line=%d, want 100 (origin)", ghosts[0].Line)
	}
	for i, g := range ghosts[1:] {
		if want := 101 + i; g.Line != want {
			t.Fatalf("band cell %d line=%d, want %d (streak must be gapless)", i, g.Line, want)
		}
	}
}

// TestTrailSweepSmoothDiagonal verifies diagonal jumps sweep as one straight,
// sub-cell smooth band: it hugs the segment between the two cursor centers,
// carries no holes, and its edges fade in quarter-cell fringes instead of
// snapping to whole cells.
func TestTrailSweepSmoothDiagonal(t *testing.T) {
	trail := testTrail()
	t0 := time.Unix(1700000000, 0)

	// (0,0) → (8,2): a shallow diagonal like a search hit or command-output
	// jump. Linear births: a sub-cell at path fraction u is born at 120ms*u.
	trail.Record(0, 0, t0)
	trail.Record(8, 2, t0.Add(time.Second))
	ghosts := trail.Ghosts(t0.Add(time.Second).Add(105 * time.Millisecond))

	covered := coveredSet(ghosts)
	// Mid-band cells are born by +105ms; the head cell (~u=0.95+) is not.
	for _, want := range [][2]int{{1, 0}, {2, 0}, {2, 1}, {3, 1}, {4, 1}, {5, 1}, {6, 1}, {6, 2}, {7, 2}} {
		if _, ok := covered[want]; !ok {
			t.Fatalf("cell (%d,%d) should be part of the band at +105ms", want[0], want[1])
		}
	}
	if _, ok := covered[[2]int{8, 2}]; ok {
		t.Fatal("head cell should not be born before ~+115ms")
	}
	// The band is a thin straight line hugging the segment: interior
	// cells carry full or near-full braille coverage, edge cells carry
	// partial braille dots. The exact braille mask depends on where the
	// line intersects the cell's 2×4 dot lattice, so we only assert
	// that interior cells have nonzero coverage.
	for _, cell := range [][2]int{{2, 0}, {2, 1}, {4, 1}, {6, 1}, {6, 2}, {7, 2}} {
		if covered[cell] == 0 {
			t.Fatalf("cell (%d,%d) should have nonzero braille coverage", cell[0], cell[1])
		}
	}
	assertBandGeometry(t, covered, [2]int{0, 0}, 0, 0, 8, 2, 1.7, 0.875)
	assertBandConnected(t, covered, [2]int{0, 0})

	// Once fully born the band reaches the cursor — the head cell fills in
	// past half coverage — and stays connected; the entry keeps Active until
	// the last staggered sub-cell fades.
	ghosts = trail.Ghosts(t0.Add(time.Second).Add(130 * time.Millisecond))
	covered = coveredSet(ghosts)
	if covered[[2]int{8, 2}] == 0 {
		t.Fatal("band should reach the cursor cell at +130ms")
	}
	if covered[[2]int{7, 2}] == 0 {
		t.Fatalf("head cell (7,2) coverage at +130ms, want non-zero")
	}
	assertBandConnected(t, covered, [2]int{0, 0})
	if !trail.Active(t0.Add(time.Second).Add(415 * time.Millisecond)) {
		t.Fatal("staggered sweep should stay Active past the fade duration")
	}
	if trail.Active(t0.Add(time.Second).Add(430 * time.Millisecond)) {
		t.Fatal("sweep should expire after duration + sweep window")
	}
}

// TestTrailSweepDiagonalSolidBand verifies a 45-degree jump sweeps a thin
// straight band along the segment — spine cells covered, nearby off-line
// cells left alone — instead of a fat staircase.
func TestTrailSweepDiagonalSolidBand(t *testing.T) {
	trail := testTrail()
	t0 := time.Unix(1700000000, 0)

	trail.Record(0, 0, t0)
	trail.Record(6, 6, t0.Add(time.Second))
	ghosts := trail.Ghosts(t0.Add(time.Second).Add(150 * time.Millisecond))

	covered := coveredSet(ghosts)
	for k := 1; k <= 6; k++ {
		if _, ok := covered[[2]int{k, k}]; !ok {
			t.Fatalf("spine cell (%d,%d) missing from the band", k, k)
		}
	}
	// Cells a full row off the line stay untouched: the band is thin.
	for _, off := range [][2]int{{0, 2}, {2, 0}, {1, 3}, {3, 1}} {
		if _, ok := covered[off]; ok {
			t.Fatalf("cell (%d,%d) is off the line and should not be covered", off[0], off[1])
		}
	}
	assertBandGeometry(t, covered, [2]int{0, 0}, 0, 0, 6, 6, 1.7, 1.0)
	assertBandConnected(t, covered, [2]int{0, 0})
}

// TestTrailSweepShallowDiagonalConnected verifies a long, mostly-horizontal
// diagonal (the command-output jump shape) sweeps as one connected band that
// follows the straight line.
func TestTrailSweepShallowDiagonalConnected(t *testing.T) {
	trail := testTrail()
	t0 := time.Unix(1700000000, 0)

	trail.Record(0, 0, t0)
	trail.Record(12, 4, t0.Add(time.Second))
	ghosts := trail.Ghosts(t0.Add(time.Second).Add(150 * time.Millisecond))

	covered := coveredSet(ghosts)
	for _, want := range [][2]int{{4, 1}, {8, 2}} {
		if _, ok := covered[want]; !ok {
			t.Fatalf("cell (%d,%d) should be part of the band", want[0], want[1])
		}
	}
	assertBandGeometry(t, covered, [2]int{0, 0}, 0, 0, 12, 4, 1.7, 1.0)
	assertBandConnected(t, covered, [2]int{0, 0})
}

// TestTrailPrunesExpired verifies the buffer does not grow without bound:
// once entries expire they are dropped, so steady-state recording keeps the
// buffer near its initial capacity.
func TestTrailPrunesExpired(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     50 * time.Millisecond,
		MaxPositions: 4,
	})
	t0 := time.Unix(1700000000, 0)

	// 100 single-cell moves at 10ms cadence: ghosts live 50ms, so only a
	// handful are ever alive. Without pruning the buffer would double-grow
	// far past its initial capacity of 4.
	for i := 0; i < 100; i++ {
		trail.Record(i, 0, t0.Add(time.Duration(i)*10*time.Millisecond))
	}

	if got := len(trail.buf); got > 16 {
		t.Fatalf("buffer grew to %d entries, pruning should keep it near 4", got)
	}
	if got := len(trail.Ghosts(t0.Add(990 * time.Millisecond))); got < 4 || got > 6 {
		t.Fatalf("expected ~5 live ghosts after pruning, got %d", got)
	}
}

// TestTrailEasedFade verifies the configured easing shapes the opacity decay.
func TestTrailEasedFade(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     300 * time.Millisecond,
		MaxPositions: 40,
		Easing:       EasingEaseOut,
	})
	t0 := time.Unix(1700000000, 0)

	// A 1-cell move (no sweep) departs (0,0) at t0+1s.
	trail.Record(0, 0, t0)
	trail.Record(1, 0, t0.Add(time.Second))

	// At half-life, ease_out gives opacity (1-0.5)^2 = 0.25.
	ghosts := trail.Ghosts(t0.Add(time.Second).Add(150 * time.Millisecond))
	if len(ghosts) != 1 {
		t.Fatalf("expected 1 ghost, got %d", len(ghosts))
	}
	if math.Abs(ghosts[0].Opacity-0.25) > 1e-9 {
		t.Fatalf("ease_out opacity at half-life = %v, want 0.25", ghosts[0].Opacity)
	}
}

// TestTrailSweepEasedStagger verifies the comet's birth times follow the
// easing's motion profile instead of a constant speed.
func TestTrailSweepEasedStagger(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     300 * time.Millisecond,
		MaxPositions: 40,
		Easing:       EasingEaseIn,
	})
	t0 := time.Unix(1700000000, 0)

	// Horizontal 4-cell jump: cell x covers path fraction u=(x-0.5)/4;
	// ease_in inverse is sqrt, so births land at 120ms*sqrt(u) — bunched
	// late (slow start, fast arrival).
	trail.Record(0, 5, t0)
	trail.Record(4, 5, t0.Add(time.Second))

	// At +20ms even the first band cell (birth ~52ms) is unborn.
	if got := len(trail.Ghosts(t0.Add(time.Second).Add(20 * time.Millisecond))); got != 1 {
		t.Fatalf("ease_in comet starts slow: expected only the origin at +20ms, got %d ghosts", got)
	}
	// By +105ms cells 1..3 are born (births 52ms, 79ms, 99.5ms); the head
	// cell 4 waits for ~108ms.
	ghosts := trail.Ghosts(t0.Add(time.Second).Add(105 * time.Millisecond))
	if len(ghosts) != 4 {
		t.Fatalf("expected origin + 3 band cells by +105ms, got %d", len(ghosts))
	}
}

// TestTrailFastMotionFillsPath verifies that fast continuous motion (several
// moves landing between frames, e.g. held keys) sweeps each move's band
// immediately, keeping the trail gapless.
func TestTrailFastMotionFillsPath(t *testing.T) {
	trail := testTrail()
	t0 := time.Unix(1700000000, 0)

	// Key-repeat cadence: 33ms between records, 3 cells per move. Both
	// sweeps are unstaggered, so their bands are born at once.
	trail.Record(0, 0, t0)
	trail.Record(3, 0, t0.Add(33*time.Millisecond))
	trail.Record(6, 0, t0.Add(66*time.Millisecond))

	ghosts := trail.Ghosts(t0.Add(80 * time.Millisecond))
	// 2 departures + 3 cells per band (interior full, head capped).
	if len(ghosts) != 8 {
		t.Fatalf("expected 8 ghosts covering both 3-cell moves, got %d: %+v", len(ghosts), ghosts)
	}
	covered := coveredSet(ghosts)
	for x := 0; x < 6; x++ {
		if _, ok := covered[[2]int{x, 0}]; !ok {
			t.Fatalf("path cell x=%d has no ghost (gap in fast-motion trail)", x)
		}
	}
	for _, g := range ghosts {
		if g.Opacity < 0.8 {
			t.Fatalf("fast-motion births are immediate, got opacity %v", g.Opacity)
		}
	}
}

// TestTrailBurstMoveFillsGap is the backspace regression test: two repeats
// landing in one frame move the cursor two cells, and the skipped cell must
// get an immediate ghost instead of leaving a hole next to the cursor.
func TestTrailBurstMoveFillsGap(t *testing.T) {
	trail := testTrail()
	t0 := time.Unix(1700000000, 0)

	// Rest at x=10, then a 2-cell leftward move within one frame.
	trail.Record(10, 5, t0)
	trail.Record(8, 5, t0.Add(33*time.Millisecond))

	ghosts := trail.Ghosts(t0.Add(33 * time.Millisecond))
	// Departure + the cell in between (full) + the head capped at its right
	// half (flat cut at the live cursor's center).
	if len(ghosts) != 3 {
		t.Fatalf("expected departure + 2 band cells, got %d: %+v", len(ghosts), ghosts)
	}
	covered := coveredSet(ghosts)
	if got := covered[[2]int{9, 5}]; got != 0xFF {
		t.Fatalf("cell (9,5) coverage %d, want full 0xFF", got)
	}
	if got := covered[[2]int{8, 5}]; got == 0 {
		t.Fatalf("head cell (8,5) coverage %d, want nonzero toward the band", got)
	}
	for _, g := range ghosts {
		if g.Opacity != 1.0 {
			t.Fatalf("fill ghosts are born immediately, got opacity %v", g.Opacity)
		}
	}
}

// TestTrailFastLongMoveFillsPath verifies a very large single-frame move
// during fast motion is also swept per cell (no gaps), with immediate births.
func TestTrailFastLongMoveFillsPath(t *testing.T) {
	trail := testTrail()
	t0 := time.Unix(1700000000, 0)

	// 20 cells in one frame with no dwell.
	trail.Record(0, 0, t0)
	trail.Record(20, 0, t0.Add(33*time.Millisecond))

	ghosts := trail.Ghosts(t0.Add(40 * time.Millisecond))
	// Departure + cells 1..19 full + the head capped at its left half.
	if len(ghosts) != 21 {
		t.Fatalf("expected departure + 20 band cells, got %d: %+v", len(ghosts), ghosts)
	}
	covered := coveredSet(ghosts)
	for x := 1; x < 20; x++ {
		if covered[[2]int{x, 0}] != 0xFF {
			t.Fatalf("path cell x=%d coverage %d, want full 0xFF (gap in fast-motion trail)", x, covered[[2]int{x, 0}])
		}
	}
	for _, g := range ghosts {
		if g.Opacity < 0.95 {
			t.Fatalf("fast-motion fill births are immediate, got opacity %v", g.Opacity)
		}
	}
}

// TestEasingCurves checks the easing functions at their landmarks.
func TestEasingCurves(t *testing.T) {
	cases := []struct {
		name string
		e    Easing
		u    float64
		want float64
		tol  float64
	}{
		{"linear half", EasingLinear, 0.5, 0.5, 1e-9},
		{"ease_in half", EasingEaseIn, 0.5, 0.25, 1e-9},
		{"ease_out half", EasingEaseOut, 0.5, 0.75, 1e-9},
		{"smoothstep half", EasingEaseInOut, 0.5, 0.5, 1e-9},
		{"clamped low", EasingEaseOut, -0.5, 0, 1e-9},
		{"clamped high", EasingEaseIn, 1.5, 1, 1e-9},
	}
	for _, tc := range cases {
		if got := tc.e.apply(tc.u); math.Abs(got-tc.want) > tc.tol {
			t.Errorf("%s: apply(%v) = %v, want %v", tc.name, tc.u, got, tc.want)
		}
	}
}

// TestEasingInverse verifies the motion inverse is monotonic and inverts the
// easing curve.
func TestEasingInverse(t *testing.T) {
	easings := []Easing{EasingLinear, EasingEaseIn, EasingEaseOut, EasingEaseInOut}
	for _, e := range easings {
		prev := 0.0
		for i := 1; i <= 20; i++ {
			u := float64(i) / 21
			inv := e.easeInverse(u)
			if inv < prev-1e-9 {
				t.Fatalf("easing %d: easeInverse not monotonic at u=%v", e, u)
			}
			if back := e.apply(inv); math.Abs(back-u) > 1e-6 {
				t.Fatalf("easing %d: apply(easeInverse(%v)) = %v, want ~%v", e, u, back, u)
			}
			prev = inv
		}
		if e.easeInverse(0) != 0 || e.easeInverse(1) != 1 {
			t.Fatalf("easing %d: endpoints must map to 0 and 1", e)
		}
	}
}

func TestTrailWrapsAround(t *testing.T) {
	trail := New(Config{Enabled: true, Duration: 10 * time.Second, MaxPositions: 4})
	now := time.Now()
	// Depart more than one ring's worth of positions with single-cell moves
	// (no interpolation) at key-repeat cadence. The buffer grows instead of
	// evicting live ghosts.
	for i := 0; i < 10; i++ {
		trail.Record(i, 0, now.Add(time.Duration(i)*33*time.Millisecond))
	}
	ghosts := trail.Ghosts(now.Add(330 * time.Millisecond))
	if len(ghosts) != 9 {
		t.Fatalf("expected all 9 departed positions (buffer grows, no eviction), got %d", len(ghosts))
	}
	if ghosts[0].X != 0 || ghosts[8].X != 8 {
		t.Fatalf("ghosts = x %d..%d, want 0..8 in order", ghosts[0].X, ghosts[8].X)
	}
}
