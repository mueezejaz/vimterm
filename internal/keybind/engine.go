package keybind

import (
	"sync/atomic"
	"time"
)

// Result describes what happened when a key was fed to the engine.
type Result int

const (
	// Matched: the key completed a binding; the action should run.
	Matched Result = iota
	// Waiting: the key extends a prefix; more keys may complete a binding.
	Waiting
	// NoMatch: the key (and any flushed pending prefix) matches nothing.
	NoMatch
)

// Engine routes keys through the mode-specific keymaps and tracks partial
// multi-key sequences.
type Engine struct {
	keymaps atomic.Pointer[map[string]*Keymap]
	timeout atomic.Int64 // nanoseconds; <= 0 disables the timeout

	// The following state is only touched from the feeding goroutine (the
	// main loop), so it needs no locking.
	pending     []Key
	pendingMode string
	lastKey     time.Time
	flushed     bool
	lastSeq     []Key // the full sequence that produced the last Matched
}

// NewEngine creates an engine with no keymaps.
func NewEngine() *Engine {
	empty := map[string]*Keymap{}
	e := &Engine{flushed: false}
	e.keymaps.Store(&empty)
	return e
}

// SetKeymaps atomically replaces the mode keymaps.
func (e *Engine) SetKeymaps(km map[string]*Keymap) {
	e.keymaps.Store(&km)
}

// SetTimeout sets the time a partial sequence may wait for completion.
// Values <= 0 disable the timeout.
func (e *Engine) SetTimeout(d time.Duration) {
	e.timeout.Store(int64(d))
}

// Flushed reports whether the previous Feed call discarded a pending
// sequence (dead end or timeout).
func (e *Engine) Flushed() bool {
	return e.flushed
}

// Feed processes one key in the given mode and returns the outcome.
func (e *Engine) Feed(mode string, k Key) (Result, Action) {
	e.flushed = false
	km := (*e.keymaps.Load())[mode]
	if km == nil {
		return NoMatch, ""
	}

	// A pending sequence is invalidated by a mode change or timeout.
	if len(e.pending) > 0 {
		timeout := time.Duration(e.timeout.Load())
		if e.pendingMode != mode || (timeout > 0 && time.Since(e.lastKey) > timeout) {
			e.pending = e.pending[:0]
			e.flushed = true
		}
	}

	candidate := make([]Key, 0, len(e.pending)+1)
	candidate = append(candidate, e.pending...)
	candidate = append(candidate, k)

	// Prefer waiting for a longer binding when the candidate is both an exact
	// match and a prefix (e.g. "g" alone vs. "gg"), like Vim does.
	if km.IsPrefix(candidate) {
		e.pending = candidate
		e.pendingMode = mode
		e.lastKey = time.Now()
		return Waiting, ""
	}
	if a, ok := km.Lookup(candidate); ok {
		e.pending = e.pending[:0]
		e.lastSeq = append(e.lastSeq[:0], candidate...)
		return Matched, a
	}

	// Dead end: discard the pending prefix and replay the key fresh.
	e.pending = e.pending[:0]
	e.flushed = true
	if km.IsPrefix([]Key{k}) {
		e.pending = []Key{k}
		e.pendingMode = mode
		e.lastKey = time.Now()
		return Waiting, ""
	}
	if a, ok := km.Lookup([]Key{k}); ok {
		e.lastSeq = append(e.lastSeq[:0], k)
		return Matched, a
	}
	return NoMatch, ""
}

// LastSeq returns the key sequence that produced the most recent Matched
// result, or nil if there has been none.
func (e *Engine) LastSeq() []Key {
	if len(e.lastSeq) == 0 {
		return nil
	}
	return e.lastSeq
}

// Pending returns a copy of the keys currently forming a partial sequence.
func (e *Engine) Pending() []Key {
	out := make([]Key, len(e.pending))
	copy(out, e.pending)
	return out
}