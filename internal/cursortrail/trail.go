// Package cursortrail tracks recent cursor positions to render a fading trail.
package cursortrail

import (
	"math"
	"sort"
	"sync"
	"time"
)

// Easing selects how ghosts fade and how the jump sweep travels. The names
// describe the motion profile: ease_out starts fast and settles into the
// cursor, ease_in starts slow and accelerates, ease_in_out does both.
type Easing int

const (
	EasingLinear Easing = iota
	EasingEaseIn
	EasingEaseOut
	EasingEaseInOut
)

// apply maps progress t in [0,1] through the easing curve. The fade uses it
// directly: opacity = 1 - apply(age/duration).
func (e Easing) apply(t float64) float64 {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	switch e {
	case EasingEaseIn:
		return t * t
	case EasingEaseOut:
		return 1 - (1-t)*(1-t)
	case EasingEaseInOut:
		return t * t * (3 - 2*t) // smoothstep
	default:
		return t
	}
}

// easeInverse maps spatial progress u in [0,1] to the time fraction at which
// motion governed by the easing reaches u. The sweep uses it to stagger the
// ghost births: birth(u) = sweepWindow * easeInverse(u).
func (e Easing) easeInverse(u float64) float64 {
	if u < 0 {
		u = 0
	} else if u > 1 {
		u = 1
	}
	switch e {
	case EasingEaseIn:
		return math.Sqrt(u)
	case EasingEaseOut:
		return 1 - math.Sqrt(1-u)
	case EasingEaseInOut:
		// Inverse smoothstep via Newton iterations; u is interior so the
		// derivative never vanishes in practice.
		t := u
		for i := 0; i < 8; i++ {
			d := 6 * t * (1 - t)
			if d < 1e-6 {
				break
			}
			t -= (t*t*(3-2*t) - u) / d
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
		}
		return t
	default:
		return u
	}
}

// Ghost is one cell of the trail with its rendering opacity. Line is an
// absolute buffer line (scrollback + screen), resolved to a viewport row by
// the caller at draw time so ghosts stay glued to their line while scrolling.
// Mask narrows the ghost to quarters of the cell so sweep edges render
// smoothly: bit0 upper-left, bit1 upper-right, bit2 lower-left, bit3
// lower-right; 0 paints the whole cell.
// BrailleMask is an 8-bit braille dot pattern (U+2800+mask) for 2x4
// sub-cell rendering; when nonzero it takes precedence over Mask.
// MinDist records the closest distance any dot in this cell got to the
// centerline, used for soft edge fading.
type Ghost struct {
	X, Line     int
	Opacity     float64 // 1.0 = full, 0.0 = invisible
	Mask        uint8
	BrailleMask uint8   // 2x4 braille dots; 0 = use quarter-cell Mask
	MinDist     float64 // closest dot-to-centerline distance for edge fade
}

// Config holds trail parameters.
type Config struct {
	Enabled      bool
	Duration     time.Duration // how long a ghost lives
	MaxPositions int           // initial capacity; the buffer grows on demand
	Easing       Easing
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:      true,
		Duration:     300 * time.Millisecond,
		MaxPositions: 40,
		Easing:       EasingLinear,
	}
}

// trailMaxEntries caps buffer growth so a pathological cursor stream cannot
// allocate without bound. 4096 entries is far more than any visible trail.
const trailMaxEntries = 4096

const (
	// trailSweepMinDwell is how long the cursor must rest at a position
	// before departing it with a long jump spawns a staggered sweep. It
	// keeps the comet reserved for real leaps; continuous motion fills the
	// path instantly instead.
	trailSweepMinDwell = 64 * time.Millisecond
)

// Trail maintains a ring buffer of recent cursor positions. It is safe for
// concurrent use: SetConfig is called from the config-watcher goroutine while
// the main loop records and reads ghosts.
type Trail struct {
	mu       sync.Mutex
	cfg      Config
	buf      []entry
	head     int
	count    int
	lastX    int
	lastLine int
	lastT    time.Time // when the current position was recorded
	hasPos   bool
}

type entry struct {
	x, line int
	t       time.Time
	// sweep entries describe a whole jump: the band between (x, line) and
	// (x1, line1) is expanded into per-cell ghosts at read time. Plain
	// entries are single departed positions.
	sweep     bool
	stagger   bool
	x1, line1 int
}

// New creates a trail with the given config.
func New(cfg Config) *Trail {
	cap := cfg.MaxPositions
	if cap < 4 {
		cap = 4
	}
	return &Trail{
		cfg: cfg,
		buf: make([]entry, cap),
	}
}

// SetConfig updates the trail config. The buffer only grows: shrinking it
// would wipe ghosts recorded under a larger capacity, and the watcher
// re-applies the config once per second.
func (t *Trail) SetConfig(cfg Config) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cap := cfg.MaxPositions
	if cap < 4 {
		cap = 4
	}
	if cap > len(t.buf) || !t.cfg.Enabled && cfg.Enabled {
		t.cfg = cfg
		t.buf = make([]entry, cap)
		t.head = 0
		t.count = 0
		t.hasPos = false
		return
	}
	t.cfg = cfg
}

// Reset clears the trail (e.g. on tab switch or mode change).
func (t *Trail) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.head = 0
	t.count = 0
	t.hasPos = false
}

// Enabled reports whether the trail is active.
func (t *Trail) Enabled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cfg.Enabled
}

// Duration returns the configured ghost lifetime.
func (t *Trail) Duration() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cfg.Duration
}

// Record registers the cursor position for this frame. It should be called
// once per render frame with the position of whichever cursor is live (the
// virtual cursor in normal/visual mode, the shell cursor in insert mode).
// A position becomes a ghost when the cursor leaves it — the fade starts at
// departure — so a move made after sitting still still animates. A jump of
// two or more cells also records a sweep: the straight band between the two
// positions is expanded into per-cell ghosts at sub-cell resolution, so
// diagonal leaps read as one smooth motion instead of a staircase.
func (t *Trail) Record(x, line int, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.cfg.Enabled {
		return
	}
	if t.hasPos && x == t.lastX && line == t.lastLine {
		return
	}
	if t.hasPos {
		// The cursor just departed (lastX, lastLine): that position turns
		// into a ghost whose fade starts now.
		t.append(t.lastX, t.lastLine, now)
		dx, dl := x-t.lastX, line-t.lastLine
		if dx < 0 {
			dx = -dx
		}
		if dl < 0 {
			dl = -dl
		}
		d := dx
		if dl > dx {
			d = dl
		}
		if d >= 2 {
			t.appendSweep(t.lastX, t.lastLine, x, line, now,
				now.Sub(t.lastT) >= trailSweepMinDwell)
		}
	}
	t.lastX, t.lastLine = x, line
	t.lastT = now
	t.hasPos = true
}

// sweepWindow is how long a staggered sweep keeps spawning sub-cell ghosts
// after the jump: the comet's head chases the cursor over this window.
func (t *Trail) sweepWindow() time.Duration {
	return t.cfg.Duration * 2 / 5
}

// lifetime is how long an entry can still contribute ghosts. Staggered
// sweeps outlive a plain departed position by the sweep window.
func (t *Trail) lifetime(e entry) time.Duration {
	l := t.cfg.Duration
	if e.sweep && e.stagger {
		l += t.sweepWindow()
	}
	return l
}

// reserve makes room for one more entry. A full buffer never evicts a live
// ghost: the oldest entry is dropped when it has already expired by this
// entry's birth time, otherwise the buffer grows.
func (t *Trail) reserve(at time.Time) {
	if t.count == len(t.buf) {
		front := (t.head - t.count + len(t.buf)) % len(t.buf)
		if at.Sub(t.buf[front].t) > t.lifetime(t.buf[front]) {
			t.count--
		} else if len(t.buf) < trailMaxEntries {
			t.grow(len(t.buf) * 2)
		}
	}
}

func (t *Trail) append(x, line int, at time.Time) {
	t.reserve(at)
	t.buf[t.head] = entry{x: x, line: line, t: at}
	t.head = (t.head + 1) % len(t.buf)
	t.count++
}

func (t *Trail) appendSweep(x0, line0, x1, line1 int, at time.Time, stagger bool) {
	t.reserve(at)
	t.buf[t.head] = entry{x: x0, line: line0, x1: x1, line1: line1, t: at,
		sweep: true, stagger: stagger}
	t.head = (t.head + 1) % len(t.buf)
	t.count++
}

// grow re-linearizes the ring into a larger buffer, preserving entry order.
func (t *Trail) grow(newCap int) {
	if newCap > trailMaxEntries {
		newCap = trailMaxEntries
	}
	if newCap <= len(t.buf) {
		return
	}
	buf := make([]entry, newCap)
	for i := 0; i < t.count; i++ {
		buf[i] = t.buf[(t.head-t.count+i+len(t.buf))%len(t.buf)]
	}
	t.buf = buf
	t.head = t.count % newCap
}

// Ghosts returns the trail positions to draw, oldest first. Plain entries
// expand to a single full-cell ghost; sweep entries rasterize their band
// into per-cell ghosts with sub-cell masks. Each ghost's opacity runs from 1
// (just born) to 0 (expired) along the configured easing curve. Oldest-first
// ordering lets the caller overwrite a shared cell with a newer ghost.
func (t *Trail) Ghosts(now time.Time) []Ghost {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.cfg.Enabled || t.count == 0 {
		return nil
	}
	dur := t.cfg.Duration
	if dur <= 0 {
		return nil
	}

	var result []Ghost
	for i := t.count - 1; i >= 0; i-- {
		idx := (t.head - 1 - i + len(t.buf)) % len(t.buf)
		e := t.buf[idx]
		age := now.Sub(e.t)
		if age < 0 || age > t.lifetime(e) {
			continue
		}
		if e.sweep {
			result = append(result, t.sweepGhosts(e, now)...)
			continue
		}
		op := 1.0 - t.cfg.Easing.apply(float64(age)/float64(dur))
		if op <= 0 {
			continue
		}
		result = append(result, Ghost{X: e.x, Line: e.line, Opacity: op})
	}
	return result
}

// sweepHalfWidth is the sweep band's half-thickness in column units (rows
// counted double, matching the on-screen cell aspect). It controls how far
// from the centerline a braille dot can be and still be covered. Dots are
// spaced 0.5 units apart in both directions, so half must exceed 0.5 to
// ensure interior cells get full braille coverage while edge cells get
// partial fringes. 1.0 gives a comfortable margin for 2×4 braille and
// produces a smooth-looking diagonal with soft edge fading.
const sweepHalfWidth = 1.0

// sweepGhosts rasterizes a jump's swept band into per-cell ghosts at
// quarter-cell resolution. The band is a thin straight segment between the
// two cursor-cell centers — thin enough that diagonals render as one line
// of quarter-blocks instead of a fat staircase — with rows counted double
// so the geometry matches the on-screen cell aspect. Every covered quarter
// joins its cell's mask, and its fade starts when the eased motion reaches
// it — the band's head chases the cursor while the tail fades at the
// origin. The departed origin cell is skipped: the plain departure ghost
// owns that cell.
func (t *Trail) sweepGhosts(e entry, now time.Time) []Ghost {
	dur := t.cfg.Duration
	// Cursor centers, rows doubled so one row = two column units.
	px, py := float64(e.x)+0.5, float64(e.line)*2+1
	qx, qy := float64(e.x1)+0.5, float64(e.line1)*2+1
	dx, dy := qx-px, qy-py
	segLen := math.Hypot(dx, dy)
	if segLen < 1 {
		return nil
	}
	half := sweepHalfWidth

	type acc struct {
		braille uint8
		op      float64
		minS    float64 // minimum path parameter among contributing samples
		minDist float64 // closest any dot in this cell got to the centerline
	}
	cells := map[[2]int]*acc{}

	// fillCell computes which of the 8 braille dots in a cell fall within
	// the sweep band and ORs them into the cell's accumulator. The band
	// is the set of points whose distance to the centerline ≤ half.
	// cellS overrides the stagger parameter; pass -1 to auto-compute
	// from the cell center's projection (used for neighbor cells).
	fillCell := func(cellX, cellY int, cellS float64) {
		if cellX < 0 || cellY < 0 {
			return
		}
		if cellX == e.x && cellY == e.line {
			return
		}
		// Reject cells whose center projects far beyond the segment
		// endpoints.
		cx := float64(cellX) + 0.5
		cy := float64(cellY)*2 + 1
		ax := (cx-px)*dx + (cy-py)*dy
		if ax < -half*segLen || ax > segLen*segLen+half*segLen {
			return
		}
		if cellS < 0 {
			cellS = ax / (segLen * segLen)
			if cellS < 0 {
				cellS = 0
			}
			if cellS > 1 {
				cellS = 1
			}
		}
		var dotMask uint8
		closestDist := half
		for row := 0; row < 4; row++ {
			for col := 0; col < 2; col++ {
				// Braille dot center in the coordinate system (rows doubled).
				dotX := float64(cellX) + float64(col)*0.5 + 0.25
				dotY := float64(cellY)*2 + float64(row)*0.5 + 0.25
				// Project dot onto centerline, clamp to segment.
				dxp := dotX - px
				dyp := dotY - py
				proj := (dxp*dx + dyp*dy) / (segLen * segLen)
				if proj < 0 {
					proj = 0
				}
				if proj > 1 {
					proj = 1
				}
				onX := px + dx*proj
				onY := py + dy*proj
				dist := math.Hypot(dotX-onX, dotY-onY)
				if dist <= half {
					dotMask |= 1 << (row*2 + col)
					if dist < closestDist {
						closestDist = dist
					}
				}
			}
		}
		if dotMask == 0 {
			return
		}
		key := [2]int{cellX, cellY}
		a := cells[key]
		if a == nil {
			a = &acc{minS: cellS, minDist: closestDist}
			cells[key] = a
		}
		a.braille |= dotMask
		if cellS < a.minS {
			a.minS = cellS
		}
		if closestDist < a.minDist {
			a.minDist = closestDist
		}
	}

	// Walk the centerline at fine intervals. At each step, determine which
	// cell the point is in and compute which of its 8 braille dots fall
	// within the sweep band. The DDA walk ensures every cell the line
	// passes through is visited (no staircase gaps), and the per-dot
	// distance check gives exact sub-cell coverage without perpendicular
	// offset leakage into adjacent cells.
	numSteps := int(segLen*8) + 1
	if numSteps < 16 {
		numSteps = 16
	}
	step := 1.0 / float64(numSteps)

	for i := 0; i < numSteps; i++ {
		s := float64(i) * step
		cx := px + dx*s
		cy := py + dy*s
		cellX := int(math.Floor(cx))
		cellY := int(math.Floor(cy / 2))
		fillCell(cellX, cellY, s)
		// On diagonals the centerline may clip a cell corner without
		// landing inside it, leaving a connectivity gap. Check the
		// perpendicular neighbor only when the line is diagonal enough
		// that the centerline truly passes through adjacent cells.
		// Neighbors use cellS=-1 (auto-computed from projection).
		if segLen > 2 && math.Abs(dx) > 0.1 && math.Abs(dy) > 0.1 {
			ratio := math.Abs(dx) / math.Abs(dy)
			if ratio > 3 {
				// Mostly horizontal: check above and below.
				fillCell(cellX, cellY-1, -1)
				fillCell(cellX, cellY+1, -1)
			} else if ratio < 0.33 {
				// Mostly vertical: check left and right.
				fillCell(cellX-1, cellY, -1)
				fillCell(cellX+1, cellY, -1)
			} else {
				// Diagonal: check all 4 orthogonal neighbors.
				fillCell(cellX-1, cellY, -1)
				fillCell(cellX+1, cellY, -1)
				fillCell(cellX, cellY-1, -1)
				fillCell(cellX, cellY+1, -1)
			}
		}
	}

	// Compute opacity per cell based on stagger timing.
	keys := make([][2]int, 0, len(cells))
	for k := range cells {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][1] != keys[j][1] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][0] < keys[j][0]
	})
	out := make([]Ghost, 0, len(keys))
	for _, k := range keys {
		a := cells[k]
		cellX, cellY := k[0], k[1]
		s := a.minS
		age := now.Sub(e.t)
		if e.stagger {
			u := s
			age = now.Sub(e.t.Add(time.Duration(float64(t.sweepWindow()) * t.cfg.Easing.easeInverse(u))))
		}
		if age < 0 {
			continue
		}
		op := 1.0 - t.cfg.Easing.apply(float64(age)/float64(dur))
		if op <= 0 {
			continue
		}
		// Edge fade: cells whose closest dot is far from the centerline
		// get their opacity reduced. This creates a soft gradient at the
		// band edges instead of a hard on/off boundary, making diagonal
		// trails look much smoother. Cells on the centerline (minDist≈0)
		// keep full opacity; only cells past the inner half of the band
		// start fading.
		edgeFade := 1.0
		if a.minDist > half*0.5 {
			edgeFade = 1.0 - 0.6*((a.minDist-half*0.5)/(half*0.5))
			if edgeFade < 0.3 {
				edgeFade = 0.3
			}
		}
		out = append(out, Ghost{X: cellX, Line: cellY, Opacity: op * edgeFade, BrailleMask: a.braille, MinDist: a.minDist})
	}
	return out
}

// Active reports whether any ghost is alive or still pending (sweep sub-cells
// stamped in the future count too). The main loop uses it to keep rendering
// frames while the trail animates and stop once it has expired.
func (t *Trail) Active(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.cfg.Enabled || t.count == 0 {
		return false
	}
	dur := t.cfg.Duration
	if dur <= 0 {
		return false
	}
	for i := 0; i < t.count; i++ {
		idx := (t.head - 1 - i + len(t.buf)) % len(t.buf)
		if now.Sub(t.buf[idx].t) <= t.lifetime(t.buf[idx]) {
			return true
		}
	}
	return false
}
