package keybind

import (
	"testing"
	"time"
)

func newTestEngine() *Engine {
	km, err := BuildKeymaps(map[string]map[string][]string{
		"normal": {
			"h":        {"move_left"},
			"gg":       {"goto_top"},
			"g":        {"goto_bottom"},
			"leader":   {"enter_insert"},
			"leader+t": {"enter_normal"},
			"zz":       {"enter_insert", "enter_normal"},
		},
		"insert": {
			"esc": {"enter_normal"},
		},
	}, NewRune(',', 0))
	if err != nil {
		panic(err)
	}
	e := NewEngine()
	e.SetKeymaps(km)
	return e
}

func h() Key     { return NewRune('h', 0) }
func g() Key     { return NewRune('g', 0) }
func x() Key     { return NewRune('x', 0) }
func comma() Key { return NewRune(',', 0) }
func kt() Key    { return NewRune('t', 0) }
func esc() Key   { return NewCode(CodeEsc, 0) }

func TestEngineSingleKey(t *testing.T) {
	e := newTestEngine()
	res, acts := e.Feed("normal", h())
	if res != Matched || len(acts) != 1 || acts[0] != ActionMoveLeft {
		t.Fatalf("Feed(h) = %v,%q, want Matched move_left", res, acts)
	}
}

func TestEngineSequence(t *testing.T) {
	e := newTestEngine()
	if res, _ := e.Feed("normal", g()); res != Waiting {
		t.Fatalf("Feed(g) = %v, want Waiting", res)
	}
	res, acts := e.Feed("normal", g())
	if res != Matched || len(acts) != 1 || acts[0] != ActionGotoTop {
		t.Fatalf("Feed(g,g) = %v,%q, want Matched goto_top", res, acts)
	}
}

func TestEngineSingleKeyAlsoBound(t *testing.T) {
	// "g" alone is bound; feeding g,g must prefer the longer "gg" binding,
	// and feeding g then h must replay h (dead end).
	e := newTestEngine()
	if res, _ := e.Feed("normal", g()); res != Waiting {
		t.Fatalf("Feed(g) = %v, want Waiting", res)
	}
	res, acts := e.Feed("normal", h())
	if res != Matched || len(acts) != 1 || acts[0] != ActionMoveLeft {
		t.Fatalf("dead-end replay: Feed(h) after g = %v,%q, want Matched move_left", res, acts)
	}
	if !e.Flushed() {
		t.Error("expected flush of pending 'g'")
	}
}

func TestEngineNoMatch(t *testing.T) {
	e := newTestEngine()
	if res, _ := e.Feed("normal", x()); res != NoMatch {
		t.Fatalf("Feed(x) = %v, want NoMatch", res)
	}
}

func TestEngineLeader(t *testing.T) {
	e := newTestEngine()
	if res, _ := e.Feed("normal", comma()); res != Waiting {
		t.Fatalf("Feed(leader) with longer binding = %v, want Waiting", res)
	}
	if res, acts := e.Feed("normal", kt()); res != Matched || len(acts) != 1 || acts[0] != ActionEnterNormal {
		t.Fatalf("Feed(leader,t) = %v,%q, want Matched enter_normal", res, acts)
	}

	// Leader followed by an unbound key is a dead end and flushes.
	e2 := newTestEngine()
	if res, _ := e2.Feed("normal", comma()); res != Waiting {
		t.Fatalf("Feed(leader) = %v, want Waiting", res)
	}
	if res, _ := e2.Feed("normal", x()); res != NoMatch {
		t.Fatalf("Feed(leader,x) = %v, want NoMatch", res)
	}
	if !e2.Flushed() {
		t.Error("expected flush after leader dead end")
	}

	// With only "leader" bound, it matches immediately.
	km := NewKeymap()
	km.Bind([]Key{comma()}, ActionEnterInsert)
	e3 := NewEngine()
	e3.SetKeymaps(map[string]*Keymap{"normal": km})
	if res, acts := e3.Feed("normal", comma()); res != Matched || len(acts) != 1 || acts[0] != ActionEnterInsert {
		t.Fatalf("Feed(leader alone) = %v,%q, want Matched enter_insert", res, acts)
	}
}

// TestEngineChain verifies a multi-action binding returns every step in
// order on a single match.
func TestEngineChain(t *testing.T) {
	e := newTestEngine()
	z := NewRune('z', 0)
	if res, _ := e.Feed("normal", z); res != Waiting {
		t.Fatalf("Feed(z) = %v, want Waiting (prefix of zz)", res)
	}
	res, acts := e.Feed("normal", z)
	if res != Matched || len(acts) != 2 || acts[0] != ActionEnterInsert || acts[1] != ActionEnterNormal {
		t.Fatalf("Feed(z,z) = %v,%q, want Matched [enter_insert enter_normal]", res, acts)
	}
}

func TestEngineTimeoutFlush(t *testing.T) {
	e := newTestEngine()
	e.SetTimeout(50 * time.Millisecond)
	if res, _ := e.Feed("normal", g()); res != Waiting {
		t.Fatalf("Feed(g) = %v, want Waiting", res)
	}
	time.Sleep(80 * time.Millisecond)
	res, acts := e.Feed("normal", h())
	if res != Matched || len(acts) != 1 || acts[0] != ActionMoveLeft {
		t.Fatalf("timeout flush: Feed(h) = %v,%q, want Matched move_left", res, acts)
	}
	if !e.Flushed() {
		t.Error("expected flush flag after timeout")
	}
}

func TestEngineModeChangeFlushes(t *testing.T) {
	e := newTestEngine()
	if res, _ := e.Feed("normal", g()); res != Waiting {
		t.Fatalf("Feed(g) = %v, want Waiting", res)
	}
	// Mode changed externally; pending must not leak across modes.
	res, acts := e.Feed("insert", x())
	if res != NoMatch || len(acts) != 0 {
		t.Fatalf("Feed(x) in insert = %v,%q, want NoMatch", res, acts)
	}
}

func TestEngineNoKeymap(t *testing.T) {
	e := NewEngine()
	if res, _ := e.Feed("normal", h()); res != NoMatch {
		t.Fatalf("Feed without keymap = %v, want NoMatch", res)
	}
}

func TestEngineInsertPassthroughKeys(t *testing.T) {
	e := newTestEngine()
	if res, _ := e.Feed("insert", x()); res != NoMatch {
		t.Fatalf("Feed(x) in insert = %v, want NoMatch (passthrough)", res)
	}
	if res, acts := e.Feed("insert", esc()); res != Matched || len(acts) != 1 || acts[0] != ActionEnterNormal {
		t.Fatalf("Feed(esc) in insert = %v,%q, want Matched enter_normal", res, acts)
	}
}
