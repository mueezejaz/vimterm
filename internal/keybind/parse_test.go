package keybind

import (
	"reflect"
	"testing"
)

func TestParseSequence(t *testing.T) {
	leader := NewRune(',', 0)
	tests := []struct {
		name  string
		in    string
		want  []Key
		err   bool
	}{
		{"single letter", "h", []Key{NewRune('h', 0)}, false},
		{"two-key sequence", "gg", []Key{NewRune('g', 0), NewRune('g', 0)}, false},
		{"uppercase key", "G", []Key{NewRune('G', ModShift)}, false},
		{"ctrl modifier", "ctrl+u", []Key{NewRune('u', ModCtrl)}, false},
		{"alt modifier", "alt+x", []Key{NewRune('x', ModAlt)}, false},
		{"combined mods", "ctrl+alt+x", []Key{NewRune('x', ModCtrl|ModAlt)}, false},
		{"esc", "esc", []Key{NewCode(CodeEsc, 0)}, false},
		{"enter", "enter", []Key{NewCode(CodeEnter, 0)}, false},
		{"space", "space", []Key{NewRune(' ', 0)}, false},
		{"ctrl+space", "ctrl+space", []Key{NewRune(' ', ModCtrl)}, false},
		{"shift+tab", "shift+tab", []Key{NewCode(CodeTab, ModShift)}, false},
		{"bare f5 is a literal sequence", "f5", []Key{NewRune('f', 0), NewRune('5', 0)}, false},
		{"f1 is a literal sequence", "f1", []Key{NewRune('f', 0), NewRune('1', 0)}, false},
		{"function key via modifier", "ctrl+f12", []Key{NewCode(CodeF12, ModCtrl)}, false},
		{"function key with shift", "shift+f7", []Key{NewCode(CodeF7, ModShift)}, false},
		{"colon", ":", []Key{NewRune(':', ModShift)}, false},
		{"leader alone", "leader", []Key{leader}, false},
		{"leader combo", "leader+t", []Key{leader, NewRune('t', 0)}, false},
		{"shift punct", "!", []Key{NewRune('!', ModShift)}, false},
		{"mixed-case sequence", "aB3", []Key{NewRune('a', 0), NewRune('B', ModShift), NewRune('3', 0)}, false},
		{"shift punct does not leak", "a!b", []Key{NewRune('a', 0), NewRune('!', ModShift), NewRune('b', 0)}, false},
		{"shift does not leak after upper", "Ga", []Key{NewRune('G', ModShift), NewRune('a', 0)}, false},
		{"empty", "", nil, true},
		{"f99 out of range", "f99", []Key{NewRune('f', 0), NewRune('9', 0), NewRune('9', 0)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSequence(tt.in, leader)
			if tt.err {
				if err == nil {
					t.Fatalf("ParseSequence(%q) expected error, got %v", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSequence(%q): %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseSequence(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildKeymaps(t *testing.T) {
	km, err := BuildKeymaps(map[string]map[string]string{
		"normal": {
			"h": "move_left",
			"j": "move_down",
			"gg": "goto_top",
		},
		"insert": {
			"esc": "enter_normal",
		},
	}, NewRune(' ', 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := km["normal"]; !ok {
		t.Fatal("missing normal keymap")
	}
	if _, ok := km["insert"]; !ok {
		t.Fatal("missing insert keymap")
	}
	if a, ok := km["normal"].Lookup([]Key{NewRune('h', 0)}); !ok || a != ActionMoveLeft {
		t.Errorf("h lookup = %q,%v", a, ok)
	}
	if a, ok := km["normal"].Lookup([]Key{NewRune('g', 0), NewRune('g', 0)}); !ok || a != ActionGotoTop {
		t.Errorf("gg lookup = %q,%v", a, ok)
	}
	if !km["normal"].IsPrefix([]Key{NewRune('g', 0)}) {
		t.Error("g must be a prefix")
	}
	if km["insert"].IsPrefix([]Key{NewRune('x', 0)}) {
		t.Error("x must not be a prefix in insert")
	}
}

func TestBuildKeymapsUnknownAction(t *testing.T) {
	_, err := BuildKeymaps(map[string]map[string]string{
		"normal": {"h": "fly_to_moon"},
	}, NewRune(' ', 0))
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestBuildKeymapsBadToken(t *testing.T) {
	_, err := BuildKeymaps(map[string]map[string]string{
		"normal": {"ctrl+": "move_left"},
	}, NewRune(' ', 0))
	if err == nil {
		t.Fatal("expected error for bad token")
	}
}

func TestKeymapBindOverride(t *testing.T) {
	km := NewKeymap()
	km.Bind([]Key{NewRune('h', 0)}, ActionMoveLeft)
	km.Bind([]Key{NewRune('h', 0)}, ActionMoveRight)
	if a, _ := km.Lookup([]Key{NewRune('h', 0)}); a != ActionMoveRight {
		t.Errorf("override failed: %q", a)
	}
}