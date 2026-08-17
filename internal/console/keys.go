package console

import "vimterm/internal/keybind"

// dwControlKeyState bits.
const (
	rightAltPressed  = 0x0001
	leftAltPressed   = 0x0002
	rightCtrlPressed = 0x0004
	leftCtrlPressed  = 0x0008
	shiftPressed     = 0x0010
)

const (
	vkBack     = 0x08
	vkTab      = 0x09
	vkReturn   = 0x0D
	vkEscape   = 0x1B
	vkSpace    = 0x20
	vkPrior    = 0x21 // PageUp
	vkNext     = 0x22 // PageDown
	vkEnd      = 0x23
	vkHome     = 0x24
	vkLeft     = 0x25
	vkUp       = 0x26
	vkRight    = 0x27
	vkDown     = 0x28
	vkInsert   = 0x2D
	vkDelete   = 0x2E
	vkA        = 0x41
	vkZ        = 0x5A
	vkF1       = 0x70
	vkF24      = 0x87
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
// to a child process through the PTY, using VT input conventions.
func KeyToBytes(k keybind.Key) []byte {
	if k.Code == keybind.CodeRune {
		r := k.Rune
		switch {
		case k.Mods&keybind.ModCtrl != 0 && r >= 'a' && r <= 'z':
			return []byte{byte(r - 'a' + 1)}
		case k.Mods&keybind.ModCtrl != 0 && r == ' ':
			return []byte{0x00}
		case k.Mods&keybind.ModCtrl != 0 && r == '[':
			return []byte{0x1B}
		case k.Mods&keybind.ModCtrl != 0 && r == '\\':
			return []byte{0x1C}
		case k.Mods&keybind.ModCtrl != 0 && r == ']':
			return []byte{0x1D}
		case k.Mods&keybind.ModCtrl != 0 && r == '^':
			return []byte{0x1E}
		case k.Mods&keybind.ModCtrl != 0 && r == '_':
			return []byte{0x1F}
		}
		return []byte(string(r))
	}

	switch k.Code {
	case keybind.CodeEnter:
		return []byte("\r")
	case keybind.CodeBackspace:
		return []byte{0x08}
	case keybind.CodeTab:
		return []byte("\t")
	case keybind.CodeEsc:
		return []byte{0x1B}
	case keybind.CodeLeft:
		return []byte("\x1b[D")
	case keybind.CodeRight:
		return []byte("\x1b[C")
	case keybind.CodeUp:
		return []byte("\x1b[A")
	case keybind.CodeDown:
		return []byte("\x1b[B")
	case keybind.CodeHome:
		return []byte("\x1b[H")
	case keybind.CodeEnd:
		return []byte("\x1b[F")
	case keybind.CodePageUp:
		return []byte("\x1b[5~")
	case keybind.CodePageDown:
		return []byte("\x1b[6~")
	case keybind.CodeInsert:
		return []byte("\x1b[2~")
	case keybind.CodeDelete:
		return []byte("\x1b[3~")
	case keybind.CodeF1, keybind.CodeF2, keybind.CodeF3, keybind.CodeF4:
		return []byte{0x1B, 'O', byte('P' + k.Code - keybind.CodeF1)}
	case keybind.CodeF5, keybind.CodeF6, keybind.CodeF7, keybind.CodeF8,
		keybind.CodeF9, keybind.CodeF10, keybind.CodeF11, keybind.CodeF12:
		params := [...]byte{15, 17, 18, 19, 20, 21, 23, 24}
		p := params[k.Code-keybind.CodeF5]
		return []byte{0x1B, '[', '0' + p/10, '0' + p%10, '~'}
	}
	return nil
}