package console

import (
	"testing"

	"vimterm/internal/keybind"
)

func TestVTParserSingleChar(t *testing.T) {
	p := &vtParser{}
	events := p.feed([]byte("a"))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ke, ok := events[0].(KeyEvent)
	if !ok {
		t.Fatalf("expected KeyEvent, got %T", events[0])
	}
	if ke.Key.Code != keybind.CodeRune || ke.Key.Rune != 'a' {
		t.Errorf("expected rune 'a', got %+v", ke.Key)
	}
}

func TestVTParserControlChars(t *testing.T) {
	p := &vtParser{}
	tests := []struct {
		input    byte
		wantCode keybind.Code
		wantRune rune
		wantMods keybind.Mods
	}{
		{0x09, keybind.CodeTab, 0, 0},
		{0x0d, keybind.CodeEnter, 0, 0},
		{0x7f, keybind.CodeBackspace, 0, 0},
		{0x01, keybind.CodeRune, 'a', keybind.ModCtrl},
		{0x1a, keybind.CodeRune, 'z', keybind.ModCtrl},
	}
	for _, tt := range tests {
		events := p.feed([]byte{tt.input})
		if len(events) != 1 {
			t.Errorf("byte 0x%02x: expected 1 event, got %d", tt.input, len(events))
			continue
		}
		ke := events[0].(KeyEvent)
		if ke.Key.Code != tt.wantCode {
			t.Errorf("byte 0x%02x: want code %d, got %d", tt.input, tt.wantCode, ke.Key.Code)
		}
		if ke.Key.Rune != tt.wantRune {
			t.Errorf("byte 0x%02x: want rune %c, got %c", tt.input, tt.wantRune, ke.Key.Rune)
		}
		if ke.Key.Mods != tt.wantMods {
			t.Errorf("byte 0x%02x: want mods %d, got %d", tt.input, tt.wantMods, ke.Key.Mods)
		}
	}
}

func TestVTParserEscapeAlone(t *testing.T) {
	p := &vtParser{}
	events := p.feed([]byte{0x1b})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ke := events[0].(KeyEvent)
	if ke.Key.Code != keybind.CodeEsc {
		t.Errorf("expected CodeEsc, got %+v", ke.Key)
	}
}

func TestVTParserArrowKeys(t *testing.T) {
	p := &vtParser{}
	tests := []struct {
		input    string
		wantCode keybind.Code
	}{
		{"\x1b[A", keybind.CodeUp},
		{"\x1b[B", keybind.CodeDown},
		{"\x1b[C", keybind.CodeRight},
		{"\x1b[D", keybind.CodeLeft},
	}
	for _, tt := range tests {
		events := p.feed([]byte(tt.input))
		if len(events) != 1 {
			t.Errorf("input %q: expected 1 event, got %d", tt.input, len(events))
			continue
		}
		ke := events[0].(KeyEvent)
		if ke.Key.Code != tt.wantCode {
			t.Errorf("input %q: want code %d, got %d", tt.input, tt.wantCode, ke.Key.Code)
		}
		if ke.Key.Mods != keybind.ModNone {
			t.Errorf("input %q: want no mods, got %d", tt.input, ke.Key.Mods)
		}
	}
}

func TestVTParserModifiedArrows(t *testing.T) {
	p := &vtParser{}
	// xterm modifier encoding: param = 1 + shift*1 + alt*2 + ctrl*4
	// So: 2=shift, 3=alt, 4=shift+alt, 5=ctrl, 6=shift+ctrl, 7=alt+ctrl
	tests := []struct {
		input    string
		wantCode keybind.Code
		wantMods keybind.Mods
	}{
		{"\x1b[1;2A", keybind.CodeUp, keybind.ModShift},
		{"\x1b[1;3B", keybind.CodeDown, keybind.ModAlt},
		{"\x1b[1;5C", keybind.CodeRight, keybind.ModCtrl},
		{"\x1b[1;6D", keybind.CodeLeft, keybind.ModShift | keybind.ModCtrl},
	}
	for _, tt := range tests {
		events := p.feed([]byte(tt.input))
		if len(events) != 1 {
			t.Errorf("input %q: expected 1 event, got %d", tt.input, len(events))
			continue
		}
		ke := events[0].(KeyEvent)
		if ke.Key.Code != tt.wantCode {
			t.Errorf("input %q: want code %d, got %d", tt.input, tt.wantCode, ke.Key.Code)
		}
		if ke.Key.Mods != tt.wantMods {
			t.Errorf("input %q: want mods %d, got %d", tt.input, tt.wantMods, ke.Key.Mods)
		}
	}
}

func TestVTParserHomeEnd(t *testing.T) {
	p := &vtParser{}
	events := p.feed([]byte("\x1b[H"))
	ke := events[0].(KeyEvent)
	if ke.Key.Code != keybind.CodeHome {
		t.Errorf("expected CodeHome, got %d", ke.Key.Code)
	}

	events = p.feed([]byte("\x1b[F"))
	ke = events[0].(KeyEvent)
	if ke.Key.Code != keybind.CodeEnd {
		t.Errorf("expected CodeEnd, got %d", ke.Key.Code)
	}
}

func TestVTParserFunctionKeys(t *testing.T) {
	p := &vtParser{}
	tests := []struct {
		input    string
		wantCode keybind.Code
	}{
		{"\x1bOP", keybind.CodeF1},
		{"\x1bOQ", keybind.CodeF2},
		{"\x1bOR", keybind.CodeF3},
		{"\x1bOS", keybind.CodeF4},
		{"\x1b[15~", keybind.CodeF5},
		{"\x1b[17~", keybind.CodeF6},
		{"\x1b[18~", keybind.CodeF7},
		{"\x1b[19~", keybind.CodeF8},
		{"\x1b[20~", keybind.CodeF9},
		{"\x1b[21~", keybind.CodeF10},
		{"\x1b[23~", keybind.CodeF11},
		{"\x1b[24~", keybind.CodeF12},
	}
	for _, tt := range tests {
		events := p.feed([]byte(tt.input))
		if len(events) != 1 {
			t.Errorf("input %q: expected 1 event, got %d", tt.input, len(events))
			continue
		}
		ke := events[0].(KeyEvent)
		if ke.Key.Code != tt.wantCode {
			t.Errorf("input %q: want code %d, got %d", tt.input, tt.wantCode, ke.Key.Code)
		}
	}
}

func TestVTParserShiftTab(t *testing.T) {
	p := &vtParser{}
	events := p.feed([]byte("\x1b[Z"))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ke := events[0].(KeyEvent)
	if ke.Key.Code != keybind.CodeTab || ke.Key.Mods != keybind.ModShift {
		t.Errorf("expected Shift+Tab, got %+v", ke.Key)
	}
}

func TestVTParserTildeKeys(t *testing.T) {
	p := &vtParser{}
	tests := []struct {
		input    string
		wantCode keybind.Code
	}{
		{"\x1b[2~", keybind.CodeInsert},
		{"\x1b[3~", keybind.CodeDelete},
		{"\x1b[5~", keybind.CodePageUp},
		{"\x1b[6~", keybind.CodePageDown},
	}
	for _, tt := range tests {
		events := p.feed([]byte(tt.input))
		if len(events) != 1 {
			t.Errorf("input %q: expected 1 event, got %d", tt.input, len(events))
			continue
		}
		ke := events[0].(KeyEvent)
		if ke.Key.Code != tt.wantCode {
			t.Errorf("input %q: want code %d, got %d", tt.input, tt.wantCode, ke.Key.Code)
		}
	}
}

func TestVTParserAltKey(t *testing.T) {
	p := &vtParser{}
	events := p.feed([]byte("\x1bx"))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ke := events[0].(KeyEvent)
	if ke.Key.Code != keybind.CodeRune || ke.Key.Rune != 'x' || ke.Key.Mods != keybind.ModAlt {
		t.Errorf("expected Alt+x, got %+v", ke.Key)
	}
}

func TestVTParserSGRMouse(t *testing.T) {
	p := &vtParser{}

	// Left press at (5,10)
	events := p.feed([]byte("\x1b[<0;6;10M"))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	me, ok := events[0].(MouseEvent)
	if !ok {
		t.Fatalf("expected MouseEvent, got %T", events[0])
	}
	if me.Button != MouseLeft || !me.Down || me.X != 5 || me.Y != 9 {
		t.Errorf("expected left press at (5,9), got %+v", me)
	}

	// Left release at (5,10)
	events = p.feed([]byte("\x1b[<0;6;10m"))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	me = events[0].(MouseEvent)
	if me.Button != MouseNone || me.Down || me.X != 5 || me.Y != 9 {
		t.Errorf("expected release at (5,9), got %+v", me)
	}
}

func TestVTParserSGRMouseWheel(t *testing.T) {
	p := &vtParser{}

	// Scroll up at (1,1)
	events := p.feed([]byte("\x1b[<64;1;1M"))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	me := events[0].(MouseEvent)
	if me.Button != MouseWheelUp {
		t.Errorf("expected MouseWheelUp, got %d", me.Button)
	}

	// Scroll down at (1,1)
	events = p.feed([]byte("\x1b[<65;1;1M"))
	me = events[0].(MouseEvent)
	if me.Button != MouseWheelDown {
		t.Errorf("expected MouseWheelDown, got %d", me.Button)
	}
}

func TestVTParserSGRMouseModifiers(t *testing.T) {
	p := &vtParser{}

	// Shift+left press at (3,5)
	events := p.feed([]byte("\x1b[<4;3;5M"))
	me := events[0].(MouseEvent)
	if me.Button != MouseLeft || !me.Down || me.Modifiers != 4 {
		t.Errorf("expected shift+left press, got %+v", me)
	}

	// Ctrl+left press at (3,5)
	events = p.feed([]byte("\x1b[<16;3;5M"))
	me = events[0].(MouseEvent)
	if me.Button != MouseLeft || !me.Down || me.Modifiers != 16 {
		t.Errorf("expected ctrl+left press, got %+v", me)
	}
}

func TestVTParserSGRMouseDrag(t *testing.T) {
	p := &vtParser{}

	// Motion with button held at (3,5)
	events := p.feed([]byte("\x1b[<32;3;5M"))
	me := events[0].(MouseEvent)
	if me.Button != MouseLeft || !me.Down || !me.Drag {
		t.Errorf("expected drag at (3,5), got %+v", me)
	}
}

func TestVTParserMultipleEvents(t *testing.T) {
	p := &vtParser{}
	events := p.feed([]byte("abc\x1b[Adef"))
	if len(events) != 7 {
		t.Fatalf("expected 7 events, got %d", len(events))
	}

	expected := []struct {
		code keybind.Code
		r    rune
	}{
		{keybind.CodeRune, 'a'},
		{keybind.CodeRune, 'b'},
		{keybind.CodeRune, 'c'},
		{keybind.CodeUp, 0},
		{keybind.CodeRune, 'd'},
		{keybind.CodeRune, 'e'},
		{keybind.CodeRune, 'f'},
	}
	for i, exp := range expected {
		ke := events[i].(KeyEvent)
		if ke.Key.Code != exp.code || (exp.r != 0 && ke.Key.Rune != exp.r) {
			t.Errorf("event %d: expected {code=%d, rune=%c}, got %+v", i, exp.code, exp.r, ke.Key)
		}
	}
}

func TestVTParserPartialSequence(t *testing.T) {
	p := &vtParser{}

	// Feed incomplete CSI sequence
	events := p.feed([]byte("\x1b["))
	if len(events) != 0 {
		t.Errorf("expected 0 events for partial CSI, got %d", len(events))
	}

	// Complete it
	events = p.feed([]byte("A"))
	if len(events) != 1 {
		t.Fatalf("expected 1 event after completion, got %d", len(events))
	}
	ke := events[0].(KeyEvent)
	if ke.Key.Code != keybind.CodeUp {
		t.Errorf("expected CodeUp, got %d", ke.Key.Code)
	}
}

func TestVTParserUTF8(t *testing.T) {
	p := &vtParser{}

	// 2-byte UTF-8: é (U+00E9) = 0xC3 0xA9
	events := p.feed([]byte{0xc3, 0xa9})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ke := events[0].(KeyEvent)
	if ke.Key.Rune != 'é' {
		t.Errorf("expected 'é', got %c", ke.Key.Rune)
	}

	// 3-byte UTF-8: 中 (U+4E2D) = 0xE4 0xB8 0xAD
	events = p.feed([]byte{0xe4, 0xb8, 0xad})
	ke = events[0].(KeyEvent)
	if ke.Key.Rune != '中' {
		t.Errorf("expected '中', got %c", ke.Key.Rune)
	}
}

func TestVTParserSS3Modified(t *testing.T) {
	p := &vtParser{}

	// SS3 with modifier: ESC O 1 ; 2 P → Shift+F1
	events := p.feed([]byte("\x1bO1;2P"))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ke := events[0].(KeyEvent)
	if ke.Key.Code != keybind.CodeF1 || ke.Key.Mods != keybind.ModShift {
		t.Errorf("expected Shift+F1, got %+v", ke.Key)
	}
}

func TestVTParserCSIWithModifier(t *testing.T) {
	p := &vtParser{}

	// Shift+End: ESC [ 1 ; 2 F
	events := p.feed([]byte("\x1b[1;2F"))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ke := events[0].(KeyEvent)
	if ke.Key.Code != keybind.CodeEnd || ke.Key.Mods != keybind.ModShift {
		t.Errorf("expected Shift+End, got %+v", ke.Key)
	}

	// Ctrl+Home: ESC [ 1 ; 5 H
	events = p.feed([]byte("\x1b[1;5H"))
	ke = events[0].(KeyEvent)
	if ke.Key.Code != keybind.CodeHome || ke.Key.Mods != keybind.ModCtrl {
		t.Errorf("expected Ctrl+Home, got %+v", ke.Key)
	}
}

func TestVTModToKeybind(t *testing.T) {
	// xterm encoding: param = 1 + shift*1 + alt*2 + ctrl*4
	// bit 0=shift, bit 1=alt, bit 2=ctrl after subtracting 1
	tests := []struct {
		code int
		want keybind.Mods
	}{
		{1, keybind.ModNone},
		{2, keybind.ModShift},
		{3, keybind.ModAlt},
		{4, keybind.ModShift | keybind.ModAlt},
		{5, keybind.ModCtrl},
		{6, keybind.ModShift | keybind.ModCtrl},
		{7, keybind.ModAlt | keybind.ModCtrl},
		{8, keybind.ModShift | keybind.ModAlt | keybind.ModCtrl},
	}
	for _, tt := range tests {
		got := vtModToKeybind(tt.code)
		if got != tt.want {
			t.Errorf("vtModToKeybind(%d) = %d, want %d", tt.code, got, tt.want)
		}
	}
}

func TestSGRMouseToEvent(t *testing.T) {
	tests := []struct {
		btn, col, row int
		release       bool
		wantBtn       MouseButton
		wantDown      bool
		wantDrag      bool
		wantX, wantY  int
	}{
		// Left press
		{0, 5, 10, false, MouseLeft, true, false, 4, 9},
		// Right press
		{2, 1, 1, false, MouseRight, true, false, 0, 0},
		// Release
		{0, 5, 10, true, MouseNone, false, false, 4, 9},
		// Wheel up
		{64, 3, 3, false, MouseWheelUp, false, false, 2, 2},
		// Wheel down
		{65, 3, 3, false, MouseWheelDown, false, false, 2, 2},
		// Motion with button
		{32, 7, 8, false, MouseLeft, true, true, 6, 7},
	}
	for _, tt := range tests {
		evt := sgrMouseToEvent(tt.btn, tt.col, tt.row, tt.release)
		if evt == nil {
			t.Errorf("btn=%d col=%d row=%d release=%v: got nil", tt.btn, tt.col, tt.row, tt.release)
			continue
		}
		if evt.Button != tt.wantBtn {
			t.Errorf("btn=%d: want button %d, got %d", tt.btn, tt.wantBtn, evt.Button)
		}
		if evt.Down != tt.wantDown {
			t.Errorf("btn=%d: want down=%v, got %v", tt.btn, tt.wantDown, evt.Down)
		}
		if evt.Drag != tt.wantDrag {
			t.Errorf("btn=%d: want drag=%v, got %v", tt.btn, tt.wantDrag, evt.Drag)
		}
		if evt.X != tt.wantX || evt.Y != tt.wantY {
			t.Errorf("btn=%d: want (%d,%d), got (%d,%d)", tt.btn, tt.wantX, tt.wantY, evt.X, evt.Y)
		}
	}
}
