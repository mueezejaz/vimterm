package console

import (
	"reflect"
	"testing"

	"vimterm/internal/emulator"
	"vimterm/internal/keybind"
)

func TestColorrefToColor(t *testing.T) {
	cases := []struct {
		in   uint32
		want emulator.Color
	}{
		{0x00ffffff, emulator.Color{R: 255, G: 255, B: 255}},
		{0x00000000, emulator.Color{}},
		{0x00ff0000, emulator.Color{B: 255}},
		{0x0000ff00, emulator.Color{G: 255}},
		{0x000000ff, emulator.Color{R: 255}},
		{0x00123456, emulator.Color{R: 0x56, G: 0x34, B: 0x12}},
	}
	for _, c := range cases {
		if got := colorrefToColor(c.in); got != c.want {
			t.Errorf("colorrefToColor(%#x) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

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
		// Modifiers must survive the trip to the child (they used to be
		// dropped, so Alt+word motions and Shift+arrows arrived unmodified).
		{"alt+b", keybind.NewRune('b', keybind.ModAlt), []byte("\x1bb")},
		{"alt+shift+b", keybind.NewRune('B', keybind.ModAlt | keybind.ModShift), []byte("\x1bB")},
		{"ctrl+alt+a", keybind.NewRune('a', keybind.ModCtrl | keybind.ModAlt), []byte{0x1B, 0x01}},
		{"shift+tab", keybind.NewCode(keybind.CodeTab, keybind.ModShift), []byte("\x1b[Z")},
		{"alt+tab", keybind.NewCode(keybind.CodeTab, keybind.ModAlt), []byte("\x1b\t")},
		{"shift+up", keybind.NewCode(keybind.CodeUp, keybind.ModShift), []byte("\x1b[1;2A")},
		{"ctrl+left", keybind.NewCode(keybind.CodeLeft, keybind.ModCtrl), []byte("\x1b[1;5D")},
		{"shift+end", keybind.NewCode(keybind.CodeEnd, keybind.ModShift), []byte("\x1b[1;2F")},
		{"ctrl+shift+home", keybind.NewCode(keybind.CodeHome, keybind.ModCtrl | keybind.ModShift), []byte("\x1b[1;6H")},
		{"shift+delete", keybind.NewCode(keybind.CodeDelete, keybind.ModShift), []byte("\x1b[3;2~")},
		{"alt+f1", keybind.NewCode(keybind.CodeF1, keybind.ModAlt), []byte("\x1b[1;3P")},
		{"shift+f5", keybind.NewCode(keybind.CodeF5, keybind.ModShift), []byte("\x1b[15;2~")},
		{"alt+enter", keybind.NewCode(keybind.CodeEnter, keybind.ModAlt), []byte("\x1b\r")},
		{"alt+backspace", keybind.NewCode(keybind.CodeBackspace, keybind.ModAlt), []byte{0x1B, 0x7F}},
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