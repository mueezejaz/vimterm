package app

import "vimterm/internal/keybind"

// countAware lists the actions that consume a numeric count prefix.
// The count is handed off (a.cnt) before the handler runs; handlers read
// it with takeCount.
var countAware = map[keybind.Action]bool{
	keybind.ActionMoveLeft:      true,
	keybind.ActionMoveRight:     true,
	keybind.ActionMoveUp:        true,
	keybind.ActionMoveDown:      true,
	keybind.ActionScrollUp:      true,
	keybind.ActionScrollDown:    true,
	keybind.ActionGotoTop:       true,
	keybind.ActionGotoBottom:    true,
	keybind.ActionSearchNext:    true,
	keybind.ActionSearchPrev:    true,
	keybind.ActionFindChar:      true,
	keybind.ActionFindCharBack:  true,
	keybind.ActionFindUntil:     true,
	keybind.ActionFindUntilBack: true,
	keybind.ActionFindNext:      true,
	keybind.ActionFindPrev:      true,
	keybind.ActionMoveWord:      true,
	keybind.ActionMoveWordBack:  true,
	keybind.ActionMoveWordEnd:   true,
	keybind.ActionMoveWORD:      true,
	keybind.ActionMoveWORDBack:  true,
	keybind.ActionMoveWORDEnd:   true,
}

// takeCount returns the count handed off before this action, or 1 when no
// count was typed.
func (a *App) takeCount() int {
	n := a.cnt
	a.cnt = 0
	if n < 1 {
		return 1
	}
	return n
}

// isDigit reports whether a key is a plain digit (no modifiers).
func isDigit(k keybind.Key) bool {
	return k.Code == keybind.CodeRune && k.Mods == 0 &&
		k.Rune >= '0' && k.Rune <= '9'
}