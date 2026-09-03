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
type Ghost struct {
	X, Line int
	Opacity float64 // 1.0 = full, 0.0 = invisible
	Mask    uint8
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

// sweepGhosts rasterizes a jump's swept band into per-cell ghosts at
// quarter-cell resolution. The band is the straight segment between the two
// cursor-cell centers, widened perpendicular to the motion by the cursor's
// own extent, with rows counted double so the geometry matches the on-screen
// cell aspect: horizontal motion smears a row tall, vertical motion a column
// wide, diagonals in between. Every covered quarter joins its cell's mask,
// and its fade starts when the eased motion reaches it — the band's head
// chases the cursor while the tail fades at the origin. The departed origin
// cell is skipped: the plain departure ghost owns that cell.
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
	// Half-thickness: the cursor rectangle (half a column by one row, i.e.
	// half-extents 0.5 x 1.0) projected onto the band's normal.
	nx, ny := -dy/segLen, dx/segLen
	half := 0.5*math.Abs(nx) + math.Abs(ny)

	type acc struct {
		mask uint8
		op   float64
	}
	cells := map[[2]int]*acc{}
	add := func(cx, cy float64) {
		if cx < 0 || cy < 0 {
			return
		}
		s := ((cx-px)*dx + (cy-py)*dy) / segLen
		if s < 0 || s > segLen {
			return
		}
		if perp := math.Abs((cx-px)*dy-(cy-py)*dx) / segLen; perp > half {
			return
		}
		cellX, cellY := int(cx), int(cy/2)
		if cellX == e.x && cellY == e.line {
			return
		}
		colBit := int(cx*2) % 2
		rowBit := int(cy) % 2
		bit := uint8(1) << (rowBit*2 + colBit)
		age := now.Sub(e.t)
		if e.stagger {
			u := s / segLen
			age = now.Sub(e.t.Add(time.Duration(float64(t.sweepWindow()) * t.cfg.Easing.easeInverse(u))))
		}
		if age < 0 {
			return
		}
		op := 1.0 - t.cfg.Easing.apply(float64(age)/float64(dur))
		if op <= 0 {
			return
		}
		key := [2]int{cellX, cellY}
		a := cells[key]
		if a == nil {
			a = &acc{}
			cells[key] = a
		}
		a.mask |= bit
		if op > a.op {
			a.op = op
		}
	}
	// Walk along the dominant axis so the per-step slice stays narrow, and
	// bracket the walk with min/max — the jump may point either way.
	if math.Abs(dx) >= math.Abs(dy) {
		lo, hi := px, qx
		if hi < lo {
			lo, hi = hi, lo
		}
		hw := half * segLen / math.Abs(dx)
		for x := int(math.Floor(lo)) - 2; x <= int(math.Floor(hi))+2; x++ {
			for _, off := range [2]float64{0.25, 0.75} {
				cx := float64(x) + off
				yc := py + (cx-px)*dy/dx
				for l := int(math.Floor((yc-hw)/2)) - 1; l <= int(math.Floor((yc+hw)/2))+1; l++ {
					add(cx, float64(l)*2+0.5)
					add(cx, float64(l)*2+1.5)
				}
			}
		}
	} else {
		lo, hi := py, qy
		if hi < lo {
			lo, hi = hi, lo
		}
		hw := half * segLen / math.Abs(dy)
		for l := int(math.Floor(lo/2)) - 2; l <= int(math.Floor(hi/2))+2; l++ {
			for _, roff := range [2]float64{0.5, 1.5} {
				cy := float64(l)*2 + roff
				xc := px + (cy-py)*dx/dy
				for x := int(math.Floor(xc-hw)) - 1; x <= int(math.Floor(xc+hw))+1; x++ {
					add(float64(x)+0.25, cy)
					add(float64(x)+0.75, cy)
				}
			}
		}
	}

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
		out = append(out, Ghost{X: k[0], Line: k[1], Opacity: a.op, Mask: a.mask})
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
