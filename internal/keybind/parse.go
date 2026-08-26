package keybind

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// specialNames maps config tokens for named keys to their Code. "space" is
// deliberately absent: the console reports it as a printable rune, so bindings
// use CodeRune with Rune ' '.
var specialNames = map[string]Code{
	"esc":       CodeEsc,
	"enter":     CodeEnter,
	"backspace": CodeBackspace,
	"tab":       CodeTab,
	"up":        CodeUp,
	"down":      CodeDown,
	"left":      CodeLeft,
	"right":     CodeRight,
	"home":      CodeHome,
	"end":       CodeEnd,
	"pageup":    CodePageUp,
	"pagedown":  CodePageDown,
	"insert":    CodeInsert,
	"delete":    CodeDelete,
}

// shiftedPunct lists punctuation that requires Shift on a US keyboard
// layout. Bindings for these characters must carry ModShift to match the
// input reported by the console.
var shiftedPunct = map[rune]bool{
	'~': true, '!': true, '@': true, '#': true, '$': true, '%': true,
	'^': true, '&': true, '*': true, '(': true, ')': true, '_': true,
	'+': true, '{': true, '}': true, '|': true, ':': true, '"': true,
	'<': true, '>': true, '?': true,
}

// ParseSequence parses a config binding token such as "h", "gg",
// "ctrl+u", "shift+tab", "leader+t", "G" or "f5" into a sequence of keys.
// A multi-character token without modifiers ("gg") is a sequence of single
// keys. The leader key itself must be passed in; the token "leader" resolves
// to it.
func ParseSequence(s string, leader Key) ([]Key, error) {
	if s == "" {
		return nil, fmt.Errorf("keybind: empty binding")
	}

	parts := strings.Split(s, "+")
	var mods Mods
	i := 0
	for ; i < len(parts); i++ {
		switch strings.ToLower(parts[i]) {
		case "ctrl":
			mods |= ModCtrl
		case "alt":
			mods |= ModAlt
		case "shift":
			mods |= ModShift
		default:
			goto keyParts
		}
	}
keyParts:
	if i >= len(parts) {
		return nil, fmt.Errorf("keybind: bad token %q", s)
	}

	var seq []Key
	for _, p := range parts[i:] {
		keys, err := parseChunk(p, mods, leader)
		if err != nil {
			return nil, err
		}
		seq = append(seq, keys...)
	}
	return seq, nil
}

// parseChunk parses one key token ("gg", "esc", "t", ...) with pre-parsed
// modifiers. "leader" resolves to the configured leader key.
func parseChunk(chunk string, mods Mods, leader Key) ([]Key, error) {
	if chunk == "" {
		return nil, fmt.Errorf("keybind: empty key in token")
	}
	lower := strings.ToLower(chunk)
	if lower == "leader" {
		leader.Mods |= mods
		return []Key{leader}, nil
	}
	if lower == "space" {
		return []Key{{Code: CodeRune, Rune: ' ', Mods: mods}}, nil
	}
	if code, ok := specialNames[lower]; ok {
		return []Key{{Code: code, Mods: mods}}, nil
	}
	// A chunk is only treated as a function key when it carries an
	// explicit modifier ("ctrl+f5"). A bare multi-rune chunk like "f5"
	// is unambiguously a sequence of literal keys (f then 5); otherwise
	// a user could never express that sequence.
	if mods != 0 && len(chunk) >= 2 && (chunk[0] == 'f' || chunk[0] == 'F') {
		if n, err := strconv.Atoi(chunk[1:]); err == nil && n >= 1 && n <= 24 {
			return []Key{{Code: CodeF1 + Code(n-1), Mods: mods}}, nil
		}
	}

	var seq []Key
	for _, r := range chunk {
		runeMods := mods
		if unicode.IsUpper(r) && mods == 0 {
			runeMods |= ModShift
		} else if shiftedPunct[r] && mods == 0 {
			runeMods |= ModShift
		}
		if mods != 0 && unicode.IsLetter(r) {
			r = unicode.ToLower(r)
		}
		seq = append(seq, Key{Code: CodeRune, Rune: r, Mods: runeMods})
	}
	return seq, nil
}

// ParseLeader parses the configured leader key, which must resolve to a
// single key.
func ParseLeader(s string) (Key, error) {
	seq, err := ParseSequence(s, Key{})
	if err != nil {
		return Key{}, err
	}
	if len(seq) != 1 {
		return Key{}, fmt.Errorf("keybind: leader %q must be a single key", s)
	}
	return seq[0], nil
}

// BuildKeymaps builds one keymap per mode from a config-style map of binding
// tokens to action-name chains. A chain usually holds one name; several run
// in order on a match. It rejects unknown actions, empty chains and
// unparseable tokens. Duplicate sequences are allowed: later entries (in
// sorted order) win.
func BuildKeymaps(bindings map[string]map[string][]string, leader Key) (map[string]*Keymap, error) {
	keymaps := make(map[string]*Keymap)
	names := make([]string, 0, len(bindings))
	for name := range bindings {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entries := bindings[name]
		keys := make([]string, 0, len(entries))
		for token := range entries {
			keys = append(keys, token)
		}
		sort.Strings(keys)

		km := NewKeymap()
		for _, token := range keys {
			raws := entries[token]
			if len(raws) == 0 {
				return nil, fmt.Errorf("keybind: [%s] %q: empty action chain", name, token)
			}
			actions := make([]Action, 0, len(raws))
			for _, raw := range raws {
				action := Action(raw)
				if !IsKnownAction(action) {
					return nil, fmt.Errorf("keybind: [%s] %q: unknown action %q", name, token, action)
				}
				actions = append(actions, action)
			}
			seq, err := ParseSequence(token, leader)
			if err != nil {
				return nil, fmt.Errorf("keybind: [%s] %q: %w", name, token, err)
			}
			km.Bind(seq, actions...)
		}
		keymaps[name] = km
	}
	return keymaps, nil
}
