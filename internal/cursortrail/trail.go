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
// partial fringes. 1.1 bridges diagonal gaps: at a 45° screen diagonal
// (doubled direction 1:2) the closest braille dots between consecutive
// cells sit ~1.006 units from the centerline, just outside 1.0, while
// cells 2 rows off the spine are at ~1.23 and stay excluded.
const sweepHalfWidth = 1.1

// sweepGhosts rasterizes a jump's swept band into per-cell ghosts using
// a DDA walk for cell identification and distance-based braille dot
// selection for coverage. The DDA samples the centerline at fine
// resolution (segLen*8 steps) so every cell the line passes through is
// visited, avoiding the cell-skipping that Bresenham exhibits on
// diagonals. For each visited cell, braille dots within the sweep band
// (distance ≤ half from the centerline) are set, producing a thin
// straight line with appropriate partial coverage at edges.
func (t *Trail) sweepGhosts(e entry, now time.Time) []Ghost {
	dur := t.cfg.Duration
	half := sweepHalfWidth

	// Centerline in doubled-y coordinates (matching original geometry).
	pxc, pyc := float64(e.x)+0.5, float64(e.line)*2+1
	qxc, qyc := float64(e.x1)+0.5, float64(e.line1)*2+1
	dxc, dyc := qxc-pxc, qyc-pyc
	segLen := math.Hypot(dxc, dyc)
	if segLen < 1e-6 {
		return nil
	}

	// DDA walk at fine resolution to identify all cells the centerline
	// passes through. Using segLen*8 steps ensures we visit every cell
	// even on steep or shallow diagonals.
	segLen8 := int(math.Ceil(segLen * 8))
	if segLen8 < 1 {
		segLen8 = 1
	}
	type cellInfo struct {
		s float64
	}
	cellVisit := map[[2]int]cellInfo{}
	// ddaCells tracks the ordered sequence of unique cells visited by the
	// DDA, preserving visitation order for diagonal bridging.
	type ddaCell struct {
		key [2]int
		s   float64
	}
	var ddaCells []ddaCell
	for i := 1; i <= segLen8; i++ {
		s := float64(i) / float64(segLen8)
		lx := pxc + dxc*s
		ly := pyc + dyc*s
		cellX := int(math.Floor(lx))
		cellY := int(math.Floor(ly) / 2)
		if cellX == e.x && cellY == e.line {
			continue
		}
		if cellX < 0 || cellY < 0 {
			continue
		}
		key := [2]int{cellX, cellY}
		if _, ok := cellVisit[key]; !ok {
			cellVisit[key] = cellInfo{s: s}
			ddaCells = append(ddaCells, ddaCell{key: key, s: s})
		}
	}

	// Bridge diagonally-adjacent DDA cells. When two consecutive DDA
	// cells differ in both x and y (diagonal step), the centerline
	// passes through the shared corner. The two cells sharing that
	// corner but not on the centerline must be included so the band
	// is 4-connected (assertBandConnected uses 4-connectivity).
	// The origin is included as the starting point so the first bridge
	// connects the origin to the first DDA cell.
	type bridgePt struct {
		key [2]int
		s   float64
	}
	bridgeSeq := make([]bridgePt, 0, len(ddaCells)+1)
	bridgeSeq = append(bridgeSeq, bridgePt{key: [2]int{e.x, e.line}, s: 0})
	for _, c := range ddaCells {
		bridgeSeq = append(bridgeSeq, bridgePt{key: c.key, s: c.s})
	}
	for i := 1; i < len(bridgeSeq); i++ {
		prev := bridgeSeq[i-1].key
		curr := bridgeSeq[i].key
		dx := curr[0] - prev[0]
		dy := curr[1] - prev[1]
		if dx != 0 && dy != 0 {
			bridges := [2][2]int{{curr[0], prev[1]}, {prev[0], curr[1]}}
			avgS := (bridgeSeq[i-1].s + bridgeSeq[i].s) / 2
			for _, bk := range bridges {
				if bk[0] < 0 || bk[1] < 0 {
					continue
				}
				if bk == [2]int{e.x1, e.line1} {
					continue
				}
				if _, ok := cellVisit[bk]; !ok {
					cellVisit[bk] = cellInfo{s: avgS}
				}
			}
		}
	}

	// For each visited cell, compute which braille dots fall within the
	// sweep band. The band is the set of points whose distance to the
	// centerline ≤ half.
	type acc struct {
		braille uint8
		minS    float64
		minDist float64
	}
	cells := map[[2]int]*acc{}

	for key, info := range cellVisit {
		cellX, cellY := key[0], key[1]
		cx := float64(cellX) + 0.5
		cy := float64(cellY)*2 + 1
		var dotMask uint8
		closestDist := half
		for row := 0; row < 4; row++ {
			for col := 0; col < 2; col++ {
				dotX := float64(cellX) + float64(col)*0.5 + 0.25
				dotY := float64(cellY)*2 + float64(row)*0.5 + 0.25
				dxp := dotX - pxc
				dyp := dotY - pyc
				proj := (dxp*dxc + dyp*dyc) / (segLen * segLen)
				if proj < 0 {
					proj = 0
				}
				if proj > 1 {
					proj = 1
				}
				onX := pxc + dxc*proj
				onY := pyc + dyc*proj
				dist := math.Hypot(dotX-onX, dotY-onY)
				if dist <= half {
					dotMask |= 1 << (col*4 + row)
					if dist < closestDist {
						closestDist = dist
					}
				}
			}
		}
		if dotMask == 0 {
			// Cell is on the path but no dots are within the band.
			// This can happen at band edges; skip the cell.
			continue
		}
		// Reject cells whose center projects far beyond the segment.
		ax := (cx-pxc)*dxc + (cy-pyc)*dyc
		if ax < -half*segLen || ax > segLen*segLen+half*segLen {
			continue
		}
		cells[key] = &acc{braille: dotMask, minS: info.s, minDist: closestDist}
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

	headS := 1.0
	if e.stagger {
		sw := t.sweepWindow()
		elapsed := now.Sub(e.t)
		if sw > 0 && elapsed >= 0 {
			progress := float64(elapsed) / float64(sw)
			if progress > 1 {
				progress = 1
			}
			headS = t.cfg.Easing.apply(progress)
		}
	}

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
		if e.stagger {
			distBehind := headS - s
			if distBehind < 0 {
				distBehind = 0
			}
			if distBehind < 0.15 {
				op *= 1.0 + 0.4*(1.0-distBehind/0.15)
			} else {
				fade := 1.0 - 0.75*((distBehind-0.15)/0.85)
				if fade < 0.25 {
					fade = 0.25
				}
				op *= fade
			}
		}
		edgeFade := 1.0
		if a.minDist > half*0.5 {
			edgeFade = 1.0 - 0.6*((a.minDist-half*0.5)/(half*0.5))
			if edgeFade < 0.3 {
				edgeFade = 0.3
			}
		}
		if op <= 0 {
			continue
		}
		mask := a.braille
		if math.Abs(dxc) > 3*math.Abs(dyc) {
			mask &= 0x66
		}
		out = append(out, Ghost{X: cellX, Line: cellY, Opacity: op * edgeFade, BrailleMask: mask, MinDist: a.minDist})
	}

	// Head ball
	if len(out) > 0 {
		hx := float64(e.x1)*2 + 1
		hy := float64(e.line1)*4 + 2
		ballX := int(math.Floor(hx)) / 2
		ballY := int(math.Floor(hy / 4))
		if ballX >= 0 && ballY >= 0 && !(ballX == e.x && ballY == e.line) {
			key := [2]int{ballX, ballY}
			if cells[key] == nil {
				sw := t.sweepWindow()
				elapsed := now.Sub(e.t)
				headAge := elapsed - time.Duration(float64(sw)*headS)
				if headAge < 0 {
					headAge = 0
				}
				headOp := 1.0 - t.cfg.Easing.apply(float64(headAge)/float64(dur))
				if headOp > 0.6 {
					headOp = 0.6 + 0.4*(headOp-0.6)/0.4
					if headOp > 1.0 {
						headOp = 1.0
					}
				}
				ballMask := uint8(0xFF)
				if math.Abs(dxc) > 3*math.Abs(dyc) {
					ballMask = 0x66
				}
				out = append(out, Ghost{X: ballX, Line: ballY, Opacity: headOp, BrailleMask: ballMask})
			}
		}
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
