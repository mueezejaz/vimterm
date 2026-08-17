package app

import "vimterm/internal/keybind"

// unitKind distinguishes what "." replays.
type unitKind int

const (
	// unitNone: nothing repeatable yet.
	unitNone unitKind = iota
	// unitKeys: a key sequence that produced a repeatable engine action.
	unitKeys
	// unitShell: the passthrough keystrokes of one insert-mode burst.
	unitShell
)

// repeatUnit is the unit "." repeats.
type repeatUnit struct {
	kind unitKind
	keys []keybind.Key
}

// nonRepeatable lists engine actions that must not overwrite the repeat
// unit: movements, mode switches, prompts, and macro bookkeeping.
var nonRepeatable = map[keybind.Action]bool{
	keybind.ActionMoveLeft:      true,
	keybind.ActionMoveRight:     true,
	keybind.ActionMoveUp:        true,
	keybind.ActionMoveDown:      true,
	keybind.ActionScrollUp:      true,
	keybind.ActionScrollDown:    true,
	keybind.ActionGotoTop:       true,
	keybind.ActionGotoBottom:    true,
	keybind.ActionSearchForward: true,
	keybind.ActionSearchNext:    true,
	keybind.ActionSearchPrev:    true,
	keybind.ActionCommandPrompt: true,
	keybind.ActionEnterInsert:   true,
	keybind.ActionEnterNormal:   true,
	keybind.ActionEnterVisual:   true,
	keybind.ActionEnterVisLine:  true,
	keybind.ActionCancelVisual:  true,
	keybind.ActionRecordMacro:   true,
	keybind.ActionPlayMacro:     true,
	keybind.ActionRepeatLast:    true,
	keybind.ActionQuit:          true,
}

// repeatTracker remembers the last repeatable unit and the current insert
// burst. It is owned by the main loop goroutine.
type repeatTracker struct {
	last  repeatUnit
	burst []keybind.Key
}

// noteAction is called after the engine matches a key sequence. Mode
// switches manage the insert burst; repeatable actions become the unit.
func (t *repeatTracker) noteAction(action keybind.Action, seq []keybind.Key) {
	switch action {
	case keybind.ActionEnterInsert:
		t.burst = nil
	case keybind.ActionEnterNormal:
		if len(t.burst) > 0 {
			t.last = repeatUnit{kind: unitShell, keys: t.burst}
			t.burst = nil
		}
	default:
		if !nonRepeatable[action] {
			t.last = repeatUnit{kind: unitKeys, keys: append([]keybind.Key(nil), seq...)}
		}
	}
}

// noteBurst records a passthrough key typed during insert mode.
func (t *repeatTracker) noteBurst(k keybind.Key) {
	t.burst = append(t.burst, k)
}

// unit returns the current repeatable unit.
func (t *repeatTracker) unit() repeatUnit {
	return t.last
}