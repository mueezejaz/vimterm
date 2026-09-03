package app

import (
	"testing"

	"vimterm/internal/keybind"
)

func TestRepeatTrackerYankBecomesUnit(t *testing.T) {
	var tr repeatTracker
	seq := []keybind.Key{keybind.NewRune('y', 0)}
	tr.noteAction(keybind.ActionYank, seq)
	u := tr.unit()
	if u.kind != unitKeys || len(u.keys) != 1 {
		t.Fatalf("unit = %+v, want unitKeys with y", u)
	}
	// The unit must be a copy, not a view of the caller's slice.
	seq[0] = keybind.NewRune('q', 0)
	if !u.keys[0].Equal(keybind.NewRune('y', 0)) {
		t.Fatalf("unit keys mutated: %+v", u.keys)
	}
}

func TestRepeatTrackerMovementDoesNotOverwrite(t *testing.T) {
	var tr repeatTracker
	tr.noteAction(keybind.ActionYank, []keybind.Key{keybind.NewRune('y', 0)})
	tr.noteAction(keybind.ActionMoveDown, []keybind.Key{keybind.NewRune('j', 0)})
	tr.noteAction(keybind.ActionScrollUp, []keybind.Key{keybind.NewRune('u', keybind.ModCtrl)})
	if u := tr.unit(); u.kind != unitKeys || len(u.keys) != 1 {
		t.Fatalf("unit overwritten by movement: %+v", u)
	}
}

func TestRepeatTrackerInsertBurst(t *testing.T) {
	var tr repeatTracker
	tr.noteBurst(keybind.NewRune('h', 0))
	tr.noteBurst(keybind.NewRune('i', 0))
	tr.noteAction(keybind.ActionEnterNormal, nil)
	u := tr.unit()
	if u.kind != unitShell || len(u.keys) != 2 {
		t.Fatalf("unit = %+v, want unitShell with 2 keys", u)
	}
}

func TestRepeatTrackerEmptyBurstNoUnit(t *testing.T) {
	var tr repeatTracker
	tr.noteAction(keybind.ActionEnterInsert, nil)
	tr.noteAction(keybind.ActionEnterNormal, nil)
	if u := tr.unit(); u.kind != unitNone {
		t.Fatalf("unit = %+v, want unitNone", u)
	}
}

func TestRepeatTrackerEnterInsertClearsBurst(t *testing.T) {
	var tr repeatTracker
	tr.noteBurst(keybind.NewRune('x', 0))
	tr.noteAction(keybind.ActionEnterInsert, nil)
	tr.noteAction(keybind.ActionEnterNormal, nil)
	if u := tr.unit(); u.kind != unitNone {
		t.Fatalf("unit = %+v, want unitNone (burst cleared)", u)
	}
}

func TestRepeatTrackerBurstAfterYank(t *testing.T) {
	var tr repeatTracker
	tr.noteAction(keybind.ActionYank, []keybind.Key{keybind.NewRune('y', 0)})
	tr.noteBurst(keybind.NewRune('x', 0))
	tr.noteAction(keybind.ActionEnterNormal, nil)
	u := tr.unit()
	if u.kind != unitShell {
		t.Fatalf("unit = %+v, want unitShell (burst wins)", u)
	}
}
