package console

import (
	"strconv"

	"vimterm/internal/keybind"
)

// dwControlKeyState bits.
const (
	rightAltPressed  = 0x0001
	leftAltPressed   = 0x0002
	rightCtrlPressed = 0x0004
	leftCtrlPressed  = 0x0008
	shiftPressed     = 0x0010
)

const (
	vkBack   = 0x08
	vkTab    = 0x09
	vkReturn = 0x0D
	vkEscape = 0x1B
	vkSpace  = 0x20
	vkPrior  = 0x21 // PageUp
	vkNext   = 0x22 // PageDown
	vkEnd    = 0x23
	vkHome   = 0x24
	vkLeft   = 0x25
	vkUp     = 0x26
	vkRight  = 0x27
	vkDown   = 0x28
	vkInsert = 0x2D
	vkDelete = 0x2E
	vkA      = 0x41
	vkZ      = 0x5A
	vkF1     = 0x70
	vkF24    = 0x87
)

// keyFromRecord converts a raw console KEY_EVENT_RECORD into a keybind.Key.
//
// This is a pure function so the mapping is unit-testable without a console.
func keyFromRecord(vk uint16, r rune, state uint32) keybind.Key {
	mods := modsFromState(state)

	switch vk {
	case vkReturn:
		return keybind.NewCode(keybind.CodeEnter, mods)
	case vkBack:
		return keybind.NewCode(keybind.CodeBackspace, mods)
	case vkTab:
		return keybind.NewCode(keybind.CodeTab, mods)
	case vkEscape:
		return keybind.NewCode(keybind.CodeEsc, mods)
	case vkSpace:
		return keybind.NewRune(' ', mods)
	case vkLeft:
		return keybind.NewCode(keybind.CodeLeft, mods)
	case vkRight:
		return keybind.NewCode(keybind.CodeRight, mods)
	case vkUp:
		return keybind.NewCode(keybind.CodeUp, mods)
	case vkDown:
		return keybind.NewCode(keybind.CodeDown, mods)
	case vkHome:
		return keybind.NewCode(keybind.CodeHome, mods)
	case vkEnd:
		return keybind.NewCode(keybind.CodeEnd, mods)
	case vkPrior:
		return keybind.NewCode(keybind.CodePageUp, mods)
	case vkNext:
		return keybind.NewCode(keybind.CodePageDown, mods)
	case vkInsert:
		return keybind.NewCode(keybind.CodeInsert, mods)
	case vkDelete:
		return keybind.NewCode(keybind.CodeDelete, mods)
	}

	if vk >= vkF1 && vk <= vkF24 {
		return keybind.NewCode(keybind.CodeF1+keybind.Code(vk-vkF1), mods)
	}

	// AltGr is reported as LEFT_CTRL+RIGHT_ALT; the UnicodeChar already holds
	// the composed character, so drop both modifiers.
	if mods&keybind.ModCtrl != 0 && mods&keybind.ModAlt != 0 {
		mods &^= keybind.ModCtrl | keybind.ModAlt
	}

	// Ctrl+letter: the console reports a control character (0x01-0x1A) as the
	// UnicodeChar; recover the letter from the virtual key code.
	if mods&keybind.ModCtrl != 0 && vk >= vkA && vk <= vkZ {
		return keybind.NewRune(rune('a')+rune(vk-vkA), mods)
	}

	// Ctrl+Space arrives as a NUL character.
	if r == 0 && vk == vkSpace {
		return keybind.NewRune(' ', mods|keybind.ModCtrl)
	}

	if r == 0 || r == '\uFFFD' {
		return keybind.Key{}
	}

	if mods&keybind.ModCtrl != 0 && r >= 1 && r <= 26 {
		return keybind.NewRune(rune('a')+r-1, mods)
	}

	return keybind.NewRune(r, mods)
}

// keyEventsFromRecord expands one key-down record into the keys it
// represents. Windows coalesces auto-repeat: holding a key produces a
// single record whose wRepeatCount is the number of presses, so each count
// must become its own event or held keys fire exactly once.
func keyEventsFromRecord(vk uint16, r rune, state uint32, repeatCount uint16) []keybind.Key {
	key := keyFromRecord(vk, r, state)
	if key.Code == 0 && key.Rune == 0 {
		return nil
	}
	n := int(repeatCount)
	if n < 1 {
		n = 1
	}
	keys := make([]keybind.Key, n)
	for i := range keys {
		keys[i] = key
	}
	return keys
}

func modsFromState(state uint32) keybind.Mods {
	var mods keybind.Mods
	if state&shiftPressed != 0 {
		mods |= keybind.ModShift
	}
	if state&(leftCtrlPressed|rightCtrlPressed) != 0 {
		mods |= keybind.ModCtrl
	}
	if state&(leftAltPressed|rightAltPressed) != 0 {
		mods |= keybind.ModAlt
	}
	return mods
}

// KeyToBytes converts a Key back into the byte sequence that should be sent
// to a child process through the PTY, using VT input conventions. Modifier
// combinations use the standard xterm encodings: an ESC prefix for Alt, and
// CSI modifier parameters (1 = shift, +2 = alt, +4 = ctrl) for special
// keys, so e.g. Shift+End reaches PSReadLine as \x1b[1;2F instead of a
// plain End.
func KeyToBytes(k keybind.Key) []byte {
	if k.Code == keybind.CodeRune {
		r := k.Rune
		var out []byte
		if k.Mods&keybind.ModAlt != 0 {
			out = append(out, 0x1B)
		}
		switch {
		case k.Mods&keybind.ModCtrl != 0 && r >= 'a' && r <= 'z':
			out = append(out, byte(r-'a'+1))
		case k.Mods&keybind.ModCtrl != 0 && r == ' ':
			out = append(out, 0x00)
		case k.Mods&keybind.ModCtrl != 0 && r == '[':
			out = append(out, 0x1B)
		case k.Mods&keybind.ModCtrl != 0 && r == '\\':
			out = append(out, 0x1C)
		case k.Mods&keybind.ModCtrl != 0 && r == ']':
			out = append(out, 0x1D)
		case k.Mods&keybind.ModCtrl != 0 && r == '^':
			out = append(out, 0x1E)
		case k.Mods&keybind.ModCtrl != 0 && r == '_':
			out = append(out, 0x1F)
		default:
			out = append(out, []byte(string(r))...)
		}
		return out
	}

	m := modParam(k)
	switch k.Code {
	case keybind.CodeEnter:
		return altPrefixed("\r", k)
	case keybind.CodeBackspace:
		// DEL (0x7F), not BS (0x08): ConPTY delivers 0x08 to the child as
		// Ctrl+Backspace (delete word), whereas 0x7F maps to a plain
		// backspace that deletes one character.
		return altPrefixed("\x7f", k)
	case keybind.CodeTab:
		switch {
		case m == 2: // Shift only: backtab
			return []byte("\x1b[Z")
		default:
			return altPrefixed("\t", k)
		}
	case keybind.CodeEsc:
		return altPrefixed("\x1b", k)
	case keybind.CodeLeft, keybind.CodeRight, keybind.CodeUp, keybind.CodeDown,
		keybind.CodeHome, keybind.CodeEnd:
		final := map[keybind.Code]byte{
			keybind.CodeLeft: 'D', keybind.CodeRight: 'C', keybind.CodeUp: 'A',
			keybind.CodeDown: 'B', keybind.CodeHome: 'H', keybind.CodeEnd: 'F',
		}[k.Code]
		if m > 0 {
			return csiParam(1, m, final)
		}
		return []byte{'\x1b', '[', final}
	case keybind.CodeInsert, keybind.CodeDelete, keybind.CodePageUp, keybind.CodePageDown:
		n := map[keybind.Code]int{
			keybind.CodeInsert: 2, keybind.CodeDelete: 3,
			keybind.CodePageUp: 5, keybind.CodePageDown: 6,
		}[k.Code]
		if m > 0 {
			return csiTilde(n, m)
		}
		return []byte("\x1b[" + strconv.Itoa(n) + "~")
	case keybind.CodeF1, keybind.CodeF2, keybind.CodeF3, keybind.CodeF4:
		final := byte('P' + k.Code - keybind.CodeF1)
		if m > 0 {
			return csiParam(1, m, final)
		}
		return []byte{0x1B, 'O', final}
	case keybind.CodeF5, keybind.CodeF6, keybind.CodeF7, keybind.CodeF8,
		keybind.CodeF9, keybind.CodeF10, keybind.CodeF11, keybind.CodeF12:
		params := [...]int{15, 17, 18, 19, 20, 21, 23, 24}
		p := params[k.Code-keybind.CodeF5]
		if m > 0 {
			return csiTilde(p, m)
		}
		s := strconv.Itoa(p)
		return []byte("\x1b[" + s + "~")
	}
	return nil
}

// modParam returns the xterm CSI modifier parameter for a key's modifiers
// (shift=1, alt=2, ctrl=4 added to a base of 1), or 0 when unmodified.
func modParam(k keybind.Key) int {
	if k.Mods == 0 {
		return 0
	}
	m := 1
	if k.Mods&keybind.ModShift != 0 {
		m += 1
	}
	if k.Mods&keybind.ModAlt != 0 {
		m += 2
	}
	if k.Mods&keybind.ModCtrl != 0 {
		m += 4
	}
	return m
}

// altPrefixed prefixes b with ESC when the key carries Alt.
func altPrefixed(s string, k keybind.Key) []byte {
	if k.Mods&keybind.ModAlt == 0 {
		return []byte(s)
	}
	return append([]byte{0x1B}, s...)
}

// csiParam builds \x1b[<p>;<m><final>.
func csiParam(p, m int, final byte) []byte {
	return []byte("\x1b[" + strconv.Itoa(p) + ";" + strconv.Itoa(m) + string(final))
}

// csiTilde builds \x1b[<n>;<m>~.
func csiTilde(n, m int) []byte {
	return []byte("\x1b[" + strconv.Itoa(n) + ";" + strconv.Itoa(m) + "~")
}
