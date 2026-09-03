// Package cursortrail tracks recent cursor positions to render a fading trail.
package cursortrail

import (
	"math"
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

// Ghost is one position in the trail with its rendering opacity. Line is an
// absolute buffer line (scrollback + screen), resolved to a viewport row by
// the caller at draw time so ghosts stay glued to their line while scrolling.
type Ghost struct {
	X, Line int
	Opacity float64 // 1.0 = full, 0.0 = invisible
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
	w       float64 // opacity weight from anti-aliased path sampling
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
// departure — so a move made after sitting still still animates. The path
// between the old and new positions is also filled one ghost per cell: a
// rested jump sweeps it with staggered births (the comet), fast motion fills
// skipped cells immediately so the trail has no holes.
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
		t.append(t.lastX, t.lastLine, now, 1)
		if now.Sub(t.lastT) >= trailSweepMinDwell {
			t.sweep(t.lastX, t.lastLine, x, line, now, true)
		} else {
			t.sweep(t.lastX, t.lastLine, x, line, now, false)
		}
	}
	t.lastX, t.lastLine = x, line
	t.lastT = now
	t.hasPos = true
}

// append adds an entry. A full buffer never evicts a live ghost: the oldest
// entry is dropped when it has already expired by this entry's birth time,
// otherwise the buffer grows.
func (t *Trail) append(x, line int, at time.Time, w float64) {
	if t.count == len(t.buf) {
		front := (t.head - t.count + len(t.buf)) % len(t.buf)
		if at.Sub(t.buf[front].t) > t.cfg.Duration {
			t.count--
		} else if len(t.buf) < trailMaxEntries {
			t.grow(len(t.buf) * 2)
		}
	}
	t.buf[t.head] = entry{x: x, line: line, t: at, w: w}
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

// sweep spawns interpolated ghosts along the straight path from
// (x0, line0) to (x1, line1). The line is rasterized one full-strength cell
// per step of its dominant axis (Bresenham), and whenever both axes step at
// once a half-weight cell bridges the corner, so diagonals read as a thin,
// connected, straight line instead of a staircase of fat runs or a mush of
// half-cells. With stagger set, sample k is born when the eased motion
// reaches its position; without it every sample is born immediately (the
// cursor passed through within the last frame). The endpoints themselves are
// excluded: the origin already became a ghost and the destination is the
// live cursor.
func (t *Trail) sweep(x0, line0, x1, line1 int, now time.Time, stagger bool) {
	dur := t.cfg.Duration
	if dur <= 0 {
		return
	}
	dx, dl := x1-x0, line1-line0
	adx, adl := dx, dl
	if adx < 0 {
		adx = -adx
	}
	if adl < 0 {
		adl = -adl
	}
	d, horizontal := adx, true
	if adl > adx {
		d, horizontal = adl, false
	}
	if d < 2 {
		return
	}
	window := dur * 2 / 5
	emit := func(x, line int, w float64, u float64) {
		if w <= 0 {
			return
		}
		at := now
		if stagger {
			at = now.Add(time.Duration(float64(window) * t.cfg.Easing.easeInverse(u)))
		}
		t.append(x, line, at, w)
	}
	// bridge connects a corner step (both axes moving) with a half-weight
	// cell so consecutive samples always share an edge.
	bridge := func(prevX, prevIr, nextX, nextIr int, u float64) {
		if prevX == nextX || prevIr == nextIr {
			return
		}
		if horizontal {
			emit(prevX, nextIr, 0.5, u)
		} else {
			emit(nextX, prevIr, 0.5, u)
		}
	}

	prevX, prevIr := x0, line0
	for k := 1; k < d; k++ {
		u := float64(k) / float64(d)
		fx := float64(x0) + float64(dx)*u
		fl := float64(line0) + float64(dl)*u
		ix, ir := int(math.Round(fx)), int(math.Round(fl))
		bridge(prevX, prevIr, ix, ir, u)
		emit(ix, ir, 1, u)
		prevX, prevIr = ix, ir
	}
	// Connect the last cell to the live cursor's cell the same way.
	bridge(prevX, prevIr, x1, line1, float64(d-1)/float64(d))
}

// Ghosts returns the trail positions to draw, oldest first. Each ghost has an
// opacity from 0 (invisible) to 1 (full), decaying along the configured
// easing curve. Entries that have expired, and sweep entries that are not
// born yet (stamped in the future), are excluded. Oldest-first ordering lets
// the caller overwrite a shared cell with the newer (more opaque) ghost.
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
		if age < 0 || age > dur {
			continue
		}
		op := (1.0 - t.cfg.Easing.apply(float64(age)/float64(dur))) * e.w
		if op <= 0 {
			continue
		}
		result = append(result, Ghost{X: e.x, Line: e.line, Opacity: op})
	}
	return result
}

// Active reports whether any ghost is alive or still pending (sweep entries
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
		if now.Sub(t.buf[idx].t) <= dur {
			return true
		}
	}
	return false
}
