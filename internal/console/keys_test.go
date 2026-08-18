package console

import (
	"reflect"
	"testing"

	"vimterm/internal/keybind"
)

func TestKeyFromRecord(t *testing.T) {
	tests := []struct {
		name  string
		vk    uint16
		r     rune
		state uint32
		want  keybind.Key
	}{
		{"letter", 'J', 'j', 0, keybind.NewRune('j', 0)},
		{"uppercase shift", 'J', 'J', shiftPressed, keybind.NewRune('J', keybind.ModShift)},
		{"ctrl+letter", 'J', 0x0A, leftCtrlPressed, keybind.NewRune('j', keybind.ModCtrl)},
		{"ctrl+shift+letter", 'J', 0x0A, leftCtrlPressed | shiftPressed, keybind.NewRune('j', keybind.ModCtrl|keybind.ModShift)},
		{"ctrl+space", vkSpace, 0, leftCtrlPressed, keybind.NewRune(' ', keybind.ModCtrl)},
		{"enter", vkReturn, 0x0D, 0, keybind.NewCode(keybind.CodeEnter, 0)},
		{"backspace", vkBack, 0x08, 0, keybind.NewCode(keybind.CodeBackspace, 0)},
		{"tab", vkTab, 0x09, 0, keybind.NewCode(keybind.CodeTab, 0)},
		{"shift+tab", vkTab, 0x09, shiftPressed, keybind.NewCode(keybind.CodeTab, keybind.ModShift)},
		{"esc", vkEscape, 0x1B, 0, keybind.NewCode(keybind.CodeEsc, 0)},
		{"arrow left", vkLeft, 0, 0, keybind.NewCode(keybind.CodeLeft, 0)},
		{"ctrl+up", vkUp, 0, leftCtrlPressed, keybind.NewCode(keybind.CodeUp, keybind.ModCtrl)},
		{"home", vkHome, 0, 0, keybind.NewCode(keybind.CodeHome, 0)},
		{"page down", vkNext, 0, 0, keybind.NewCode(keybind.CodePageDown, 0)},
		{"delete", vkDelete, 0, 0, keybind.NewCode(keybind.CodeDelete, 0)},
		{"f5", vkF1 + 4, 0, 0, keybind.NewCode(keybind.CodeF5, 0)},
		{"alt+x", 'X', 'x', leftAltPressed, keybind.NewRune('x', keybind.ModAlt)},
		{"altgr+e", 'E', 'e', leftCtrlPressed | rightAltPressed, keybind.NewRune('e', 0)},
		{"null", 0, 0, 0, keybind.Key{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keyFromRecord(tt.vk, tt.r, tt.state)
			if !got.Equal(tt.want) {
				t.Errorf("keyFromRecord(%#x, %q, %#x) = %+v, want %+v", tt.vk, tt.r, tt.state, got, tt.want)
			}
		})
	}
}

func TestKeyToBytes(t *testing.T) {
	tests := []struct {
		name string
		key  keybind.Key
		want []byte
	}{
		{"rune", keybind.NewRune('a', 0), []byte("a")},
		{"unicode", keybind.NewRune('λ', 0), []byte("λ")},
		{"ctrl+a", keybind.NewRune('a', keybind.ModCtrl), []byte{0x01}},
		{"ctrl+space", keybind.NewRune(' ', keybind.ModCtrl), []byte{0x00}},
		{"enter", keybind.NewCode(keybind.CodeEnter, 0), []byte("\r")},
		{"backspace", keybind.NewCode(keybind.CodeBackspace, 0), []byte{0x7F}},
		{"esc", keybind.NewCode(keybind.CodeEsc, 0), []byte{0x1B}},
		{"up", keybind.NewCode(keybind.CodeUp, 0), []byte("\x1b[A")},
		{"left", keybind.NewCode(keybind.CodeLeft, 0), []byte("\x1b[D")},
		{"f1", keybind.NewCode(keybind.CodeF1, 0), []byte("\x1bOP")},
		{"f5", keybind.NewCode(keybind.CodeF5, 0), []byte("\x1b[15~")},
		{"f12", keybind.NewCode(keybind.CodeF12, 0), []byte("\x1b[24~")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KeyToBytes(tt.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("KeyToBytes(%+v) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}