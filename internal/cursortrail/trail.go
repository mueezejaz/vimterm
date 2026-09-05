// Package cursortrail tracks recent cursor positions to render a fading trail.
package cursortrail

import (
	"math"
	"sort"
	"sync"
	"time"
)

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

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

	// trailSweepChainWindow is how tightly a follow-up jump must trail the
	// previous move to count as the same motion burst and extend the
	// burst's band instead of starting a new one. Slower follow-ups (a
	// deliberate second leap) stay separate sweeps.
	trailSweepChainWindow = 50 * time.Millisecond
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
	// Chain state for fast consecutive jumps: the anchor the burst's
	// straight band is drawn from, extended by each follow-up jump
	// (recordSweep). The band is re-rasterized from it every frame.
	chainX      int
	chainLine   int
	chainActive bool
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
	t.chainActive = false
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
// diagonal leaps read as one smooth motion instead of a staircase. Fast
// consecutive jumps chain into a single band anchored at the burst's start
// (recordSweep), so a diagonal burst draws one straight line rather than
// stair-stepped per-frame segments.
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
		t.recordSweep(t.lastX, t.lastLine, x, line, now)
	}
	t.lastX, t.lastLine = x, line
	t.lastT = now
	t.hasPos = true
}

// recordSweep records the move from (x0, line0) to (x, line) as a sweep band
// when it spans two or more cells. A follow-up jump within
// trailSweepChainWindow that keeps moving in the burst's direction re-anchors
// the band at the burst's start instead: the cursor's per-frame path through
// a diagonal burst is a staircase, but the trail it leaves should be the one
// straight line from where the motion began to where the cursor is now.
func (t *Trail) recordSweep(x0, line0, x, line int, now time.Time) {
	dx, dl := x-x0, line-line0
	if dx < 0 {
		dx = -dx
	}
	if dl < 0 {
		dl = -dl
	}
	if max(dx, dl) < 2 {
		return
	}
	stagger := now.Sub(t.lastT) >= trailSweepMinDwell
	if !stagger && t.chainActive && now.Sub(t.lastT) <= trailSweepChainWindow && t.chainContinues(x, line) {
		t.appendSweep(t.chainX, t.chainLine, x, line, now, false)
		return
	}
	t.appendSweep(x0, line0, x, line, now, stagger)
	t.chainX, t.chainLine = x0, line0
	t.chainActive = true
}

// chainContinues reports whether a move to (x, line) keeps the burst
// straight: still heading away from the anchor, with the new step roughly
// along the chain's direction, so one straight band keeps covering the
// motion. Direction changes (perpendicular or reversing steps) break the
// chain rather than bend it.
func (t *Trail) chainContinues(x, line int) bool {
	cdx, cdl := t.lastX-t.chainX, t.lastLine-t.chainLine
	ndx, ndl := x-t.lastX, line-t.lastLine
	return cdx*ndx+cdl*ndl > 0 &&
		cdx*(x-t.chainX)+cdl*(line-t.chainLine) > 0
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

func (t *Trail) appendEntry(e entry) {
	t.reserve(e.t)
	t.buf[t.head] = e
	t.head = (t.head + 1) % len(t.buf)
	t.count++
}

func (t *Trail) append(x, line int, at time.Time) {
	t.appendEntry(entry{x: x, line: line, t: at})
}

func (t *Trail) appendSweep(x0, line0, x1, line1 int, at time.Time, stagger bool) {
	t.appendEntry(entry{x: x0, line: line0, x1: x1, line1: line1, t: at,
		sweep: true, stagger: stagger})
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

// halfWidth is the band radius in dot units: the maximum distance a braille
// dot can be from the centerline and still be lit. Wide enough to cover both
// dot columns of a vertical or horizontal streak, thin enough to keep
// off-line cells dark.
const halfWidth = 1.2

// sweepGhosts rasterizes a jump's swept band into per-cell ghosts. The band
// is the capsule of radius halfWidth around the exact centerline segment: it
// grid-walks the cells the segment passes through (in dot space, where a
// cell spans [x,x+1) × [2y,2y+2)), then lights every braille dot within
// halfWidth of the segment in each of those cells. Deriving the cell set
// from the true line rather than a quantized pixel walk keeps the band
// straight on diagonals — no dropped cells at corners, no drift bulges. A
// staggered sweep additionally rides a solid head ball along the segment at
// the eased head position.
func (t *Trail) sweepGhosts(e entry, now time.Time) []Ghost {
	dur := t.cfg.Duration

	// Centerline in doubled-y coordinates.
	pxc, pyc := float64(e.x)+0.5, float64(e.line)*2+1
	qxc, qyc := float64(e.x1)+0.5, float64(e.line1)*2+1
	dxc, dyc := qxc-pxc, qyc-pyc
	segLen := math.Hypot(dxc, dyc)
	if segLen < 1e-6 {
		return nil
	}

	// Amanatides–Woo grid walk: the ordered cells the centerline passes
	// through. tMax* are the segment parameters of the next gridline
	// crossings (x at integers, y at even integers — a cell is two dot-y
	// units tall).
	cx, cy := e.x, e.line
	stepX, stepY := 0, 0
	tMaxX, tMaxY := math.Inf(1), math.Inf(1)
	var tDeltaX, tDeltaY float64
	if dxc > 0 {
		stepX = 1
		tMaxX = (float64(cx+1) - pxc) / dxc
		tDeltaX = 1 / dxc
	} else if dxc < 0 {
		stepX = -1
		tMaxX = (pxc - float64(cx)) / -dxc
		tDeltaX = 1 / -dxc
	}
	if dyc > 0 {
		stepY = 1
		tMaxY = (float64(2*(cy+1)) - pyc) / dyc
		tDeltaY = 2 / dyc
	} else if dyc < 0 {
		stepY = -1
		tMaxY = (pyc - float64(2*cy)) / -dyc
		tDeltaY = 2 / -dyc
	}
	path := make([][2]int, 0, abs(int(dxc))+abs(int(dyc))/2+3)
	path = append(path, [2]int{cx, cy})
	for tMaxX < 1 || tMaxY < 1 {
		if tMaxX < tMaxY {
			cx += stepX
			tMaxX += tDeltaX
		} else if tMaxY < tMaxX {
			cy += stepY
			tMaxY += tDeltaY
		} else {
			// The segment crosses a grid corner exactly: step diagonally.
			cx += stepX
			cy += stepY
			tMaxX += tDeltaX
			tMaxY += tDeltaY
		}
		path = append(path, [2]int{cx, cy})
	}

	// Light, in every walked cell, all braille dots within halfWidth of the
	// segment (projection clamped to the segment's ends, so the band is a
	// capsule). minS is the earliest projection among the cell's lit dots —
	// where the band's leading edge enters the cell — and drives staggered
	// birth times; minDist feeds the soft edge fade.
	type acc struct {
		braille uint8
		minS    float64
		minDist float64
	}
	cells := map[[2]int]*acc{}
	origin := [2]int{e.x, e.line}
	lightCell := func(key [2]int) {
		if key == origin || key[0] < 0 || key[1] < 0 {
			return
		}
		if _, ok := cells[key]; ok {
			return
		}
		var dotMask uint8
		minS, minDist := 1.0, halfWidth
		for row := 0; row < 4; row++ {
			for col := 0; col < 2; col++ {
				dotX := float64(key[0]) + float64(col)*0.5 + 0.25
				dotY := float64(key[1])*2 + float64(row)*0.5 + 0.25
				dxp := dotX - pxc
				dyp := dotY - pyc
				proj := (dxp*dxc + dyp*dyc) / (segLen * segLen)
				if proj < 0 {
					proj = 0
				}
				if proj > 1 {
					proj = 1
				}
				dist := math.Hypot(dxp-dxc*proj, dyp-dyc*proj)
				if dist > halfWidth {
					continue
				}
				dotMask |= 1 << (col*4 + row)
				if proj < minS {
					minS = proj
				}
				if dist < minDist {
					minDist = dist
				}
			}
		}
		if dotMask != 0 {
			cells[key] = &acc{braille: dotMask, minS: minS, minDist: minDist}
		}
	}
	for _, key := range path {
		lightCell(key)
	}
	// Bridge cells skipped at exact grid-corner crossings: the two
	// edge-adjacent cells keep the covered set edge-connected.
	for i := 1; i < len(path); i++ {
		prev, curr := path[i-1], path[i]
		if curr[0] != prev[0] && curr[1] != prev[1] {
			lightCell([2]int{curr[0], prev[1]})
			lightCell([2]int{prev[0], curr[1]})
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
		if a.minDist > halfWidth*0.5 {
			edgeFade = 1.0 - 0.6*((a.minDist-halfWidth*0.5)/(halfWidth*0.5))
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

	// Head ball: a solid dot rides the band's leading edge, sliding from
	// the origin to the cursor at the eased head position — exactly at the
	// staggered birth edge, so it leads the band by a hair. Appended last so
	// it paints over the band cell. Unstaggered sweeps (fast motion) leave
	// it parked on the live cursor, which paintTrailGhosts skips.
	bx, by := pxc+dxc*headS, pyc+dyc*headS
	ballX, ballY := int(math.Floor(bx)), int(math.Floor(by/2))
	if ballX >= 0 && ballY >= 0 && !(ballX == e.x && ballY == e.line) {
		ballMask := uint8(0xFF)
		if math.Abs(dxc) > 3*math.Abs(dyc) {
			ballMask = 0x66
		}
		headAge := now.Sub(e.t) - time.Duration(float64(t.sweepWindow())*headS)
		if headAge < 0 {
			headAge = 0
		}
		headOp := 1.0 - t.cfg.Easing.apply(float64(headAge)/float64(dur))
		if e.stagger {
			headOp *= 1.4 // the band's leading-edge boost: the head stays brightest
		}
		out = append(out, Ghost{X: ballX, Line: ballY, Opacity: headOp, BrailleMask: ballMask})
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
