package cursortrail

import (
	"math"
	"testing"
	"time"
)

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

// TestTrailJumpFromRestSweepsPath verifies a long jump from a rested position
// spawns a per-cell streak of interpolated ghosts that grows toward the
// destination with no gaps.
func TestTrailJumpFromRestSweepsPath(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     300 * time.Millisecond,
		MaxPositions: 40,
	})
	t0 := time.Unix(1700000000, 0)

	// Rest, then an 8-cell horizontal jump (word-motion style).
	trail.Record(2, 5, t0)
	trail.Record(10, 5, t0.Add(time.Second))

	// d=8 → 7 interior points (one per cell), dt = 300*2/(5*8) = 15ms apart.
	// Right at the jump only the departed origin exists; the streak is unborn.
	ghosts := trail.Ghosts(t0.Add(time.Second))
	if len(ghosts) != 1 || ghosts[0].X != 2 || ghosts[0].Line != 5 {
		t.Fatalf("origin ghost = %+v, want (2,5)", ghosts)
	}

	// 110ms in, all seven points are born: origin + full streak.
	ghosts = trail.Ghosts(t0.Add(time.Second).Add(110 * time.Millisecond))
	if len(ghosts) != 8 {
		t.Fatalf("expected origin + 7 sweep points at +110ms, got %d: %+v", len(ghosts), ghosts)
	}
	if ghosts[0].X != 2 {
		t.Fatalf("oldest ghost x=%d, want 2 (origin)", ghosts[0].X)
	}
	// The streak covers every cell between the endpoints: x = 3..9.
	for i, want := range []int{3, 4, 5, 6, 7, 8, 9} {
		if ghosts[i+1].X != want {
			t.Fatalf("sweep point %d x=%d, want %d (streak must be gapless)", i, ghosts[i+1].X, want)
		}
	}
	// The streak's head (nearest the cursor) is the most opaque ghost.
	if ghosts[6].Opacity <= ghosts[1].Opacity {
		t.Fatalf("sweep head opacity %v should exceed tail %v", ghosts[6].Opacity, ghosts[1].Opacity)
	}
}

// TestTrailVerticalSweepCoversPath verifies a dir-style vertical jump paints
// one ghost per line so the streak has no gaps.
func TestTrailVerticalSweepCoversPath(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     300 * time.Millisecond,
		MaxPositions: 40,
	})
	t0 := time.Unix(1700000000, 0)

	trail.Record(3, 10, t0)
	trail.Record(3, 40, t0.Add(time.Second))

	// d=30 → 29 interior samples; linear births spread over the 120ms
	// window, so all are born by +116ms.
	ghosts := trail.Ghosts(t0.Add(time.Second).Add(120 * time.Millisecond))
	if len(ghosts) != 30 {
		t.Fatalf("expected origin + 29 per-cell sweep points, got %d: %+v", len(ghosts), ghosts)
	}
	if ghosts[0].Line != 10 {
		t.Fatalf("oldest ghost line=%d, want 10 (origin)", ghosts[0].Line)
	}
	for i, g := range ghosts[1:] {
		if want := 11 + i; g.Line != want {
			t.Fatalf("sweep point %d line=%d, want %d (streak must be gapless)", i, g.Line, want)
		}
	}
}

// TestTrailSweepSmoothDiagonal verifies diagonal jumps rasterize as a thin
// Bresenham line with half-weight corner bridges, so the path is connected
// and reads as a straight line instead of a staircase or a blobby band.
func TestTrailSweepSmoothDiagonal(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     300 * time.Millisecond,
		MaxPositions: 40,
	})
	t0 := time.Unix(1700000000, 0)

	// (0,0) → (8,2): d=8, samples at (k, round(k/4)) for k=1..7 with linear
	// births at 120ms*k/8. Row changes at k=2 and k=6 spawn half-weight
	// corner bridges. At +105ms the last sample has age 0.
	trail.Record(0, 0, t0)
	trail.Record(8, 2, t0.Add(time.Second))
	ghosts := trail.Ghosts(t0.Add(time.Second).Add(105 * time.Millisecond))

	byCell := map[[2]int]float64{}
	for _, g := range ghosts {
		byCell[[2]int{g.X, g.Line}] = g.Opacity
	}
	if len(ghosts) != 10 {
		t.Fatalf("expected origin + 7 main cells + 2 bridges, got %d: %+v", len(ghosts), ghosts)
	}
	// The newest main cell is full strength.
	if got := byCell[[2]int{7, 2}]; got != 1.0 {
		t.Fatalf("cell (7,2) opacity = %v, want 1.0", got)
	}
	// A main cell two samples old fades linearly: age 75ms → 0.75.
	if got := byCell[[2]int{2, 1}]; math.Abs(got-0.75) > 1e-6 {
		t.Fatalf("cell (2,1) opacity = %v, want 0.75", got)
	}
	// Corner bridges carry half weight: (1,1) born at 30ms (age 75ms).
	if got := byCell[[2]int{1, 1}]; math.Abs(got-0.375) > 1e-6 {
		t.Fatalf("bridge cell (1,1) opacity = %v, want 0.375", got)
	}
	if got := byCell[[2]int{5, 2}]; math.Abs(got-0.475) > 1e-6 {
		t.Fatalf("bridge cell (5,2) opacity = %v, want 0.475", got)
	}
	// Every column between the endpoints carries part of the line.
	for x := 1; x < 8; x++ {
		_, c0 := byCell[[2]int{x, 0}]
		_, c1 := byCell[[2]int{x, 1}]
		_, c2 := byCell[[2]int{x, 2}]
		if !c0 && !c1 && !c2 {
			t.Fatalf("column %d has no ghost (gap in the line)", x)
		}
	}
}

// TestTrailLongJumpStaysGapless verifies a jump far longer than the initial
// ring capacity still gets one ghost per cell (the buffer grows to fit).
func TestTrailLongJumpStaysGapless(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     time.Second,
		MaxPositions: 40,
	})
	t0 := time.Unix(1700000000, 0)

	// 200-line jump: 199 interior cells, all gapless.
	trail.Record(0, 100, t0)
	trail.Record(0, 300, t0.Add(time.Second))

	ghosts := trail.Ghosts(t0.Add(time.Second).Add(500 * time.Millisecond))
	if len(ghosts) != 200 {
		t.Fatalf("expected origin + 199 per-cell points, got %d", len(ghosts))
	}
	if ghosts[0].Line != 100 {
		t.Fatalf("oldest ghost line=%d, want 100 (origin)", ghosts[0].Line)
	}
	for i, g := range ghosts[1:] {
		if want := 101 + i; g.Line != want {
			t.Fatalf("sweep point %d line=%d, want %d (streak must be gapless)", i, g.Line, want)
		}
	}
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

	// d=4 → 3 interior points at u=0.25, 0.5, 0.75. ease_in inverse is
	// sqrt: births at 120ms * sqrt(u) ≈ 60ms, 84.9ms, 103.9ms — bunched
	// late (slow start, fast arrival).
	trail.Record(0, 5, t0)
	trail.Record(4, 5, t0.Add(time.Second))

	if got := len(trail.Ghosts(t0.Add(time.Second).Add(50 * time.Millisecond))); got != 1 {
		t.Fatalf("ease_in comet starts slow: expected only the origin at +50ms, got %d ghosts", got)
	}
	ghosts := trail.Ghosts(t0.Add(time.Second).Add(105 * time.Millisecond))
	if len(ghosts) != 4 {
		t.Fatalf("expected origin + 3 points by +105ms, got %d", len(ghosts))
	}
}

// TestTrailFastMotionFillsPath verifies that fast continuous motion (several
// moves landing between frames, e.g. held keys) fills the skipped cells
// immediately, keeping the trail gapless.
func TestTrailFastMotionFillsPath(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     300 * time.Millisecond,
		MaxPositions: 40,
	})
	t0 := time.Unix(1700000000, 0)

	// Key-repeat cadence: 33ms between records, 3 cells per move.
	trail.Record(0, 0, t0)
	trail.Record(3, 0, t0.Add(33*time.Millisecond))
	trail.Record(6, 0, t0.Add(66*time.Millisecond))

	ghosts := trail.Ghosts(t0.Add(80 * time.Millisecond))
	if len(ghosts) != 6 {
		t.Fatalf("expected 6 ghosts covering both 3-cell moves, got %d: %+v", len(ghosts), ghosts)
	}
	seen := map[int]bool{}
	for _, g := range ghosts {
		seen[g.X] = true
	}
	for x := 0; x < 6; x++ {
		if !seen[x] {
			t.Fatalf("path cell x=%d has no ghost (gap in fast-motion trail)", x)
		}
	}
}

// TestTrailBurstMoveFillsGap is the backspace regression test: two repeats
// landing in one frame move the cursor two cells, and the skipped cell must
// get an immediate ghost instead of leaving a hole next to the cursor.
func TestTrailBurstMoveFillsGap(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     300 * time.Millisecond,
		MaxPositions: 40,
	})
	t0 := time.Unix(1700000000, 0)

	// Rest at x=10, then a 2-cell leftward move within one frame.
	trail.Record(10, 5, t0)
	trail.Record(8, 5, t0.Add(33*time.Millisecond))

	ghosts := trail.Ghosts(t0.Add(33 * time.Millisecond))
	if len(ghosts) != 2 {
		t.Fatalf("expected departure + 1 fill ghost, got %d: %+v", len(ghosts), ghosts)
	}
	seen := map[int]bool{}
	for _, g := range ghosts {
		seen[g.X] = true
		if g.Opacity != 1.0 {
			t.Fatalf("fill ghosts are born immediately, got opacity %v", g.Opacity)
		}
	}
	if !seen[9] || !seen[10] {
		t.Fatalf("ghosts at x=9 and x=10 expected, got %+v", seen)
	}
}

// TestTrailFastLongMoveFillsPath verifies a very large single-frame move
// during fast motion is also filled per cell (no gaps), with immediate births.
func TestTrailFastLongMoveFillsPath(t *testing.T) {
	trail := New(Config{
		Enabled:      true,
		Duration:     300 * time.Millisecond,
		MaxPositions: 40,
	})
	t0 := time.Unix(1700000000, 0)

	// 20 cells in one frame with no dwell.
	trail.Record(0, 0, t0)
	trail.Record(20, 0, t0.Add(33*time.Millisecond))

	ghosts := trail.Ghosts(t0.Add(40 * time.Millisecond))
	if len(ghosts) != 20 {
		t.Fatalf("expected 1 departure + 19 fill ghosts, got %d: %+v", len(ghosts), ghosts)
	}
	seen := map[int]bool{}
	for _, g := range ghosts {
		seen[g.X] = true
		if g.Opacity < 0.95 {
			t.Fatalf("fast-motion fill births are immediate, got opacity %v", g.Opacity)
		}
	}
	for x := 1; x < 20; x++ {
		if !seen[x] {
			t.Fatalf("path cell x=%d has no ghost (gap in fast-motion trail)", x)
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
