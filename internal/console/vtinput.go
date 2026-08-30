package console

import (
	"strconv"
	"strings"

	"vimterm/internal/keybind"
)

// vtParser is a streaming VT input parser. It accumulates partial escape
// sequences across reads and emits key/mouse events from the raw byte stream
// delivered by ReadFile when ENABLE_VIRTUAL_TERMINAL_INPUT is active.
type vtParser struct {
	partial []byte // incomplete sequence carried across reads
}

// feed appends new data to the parser and returns all parseable events.
func (p *vtParser) feed(data []byte) []Event {
	buf := append(p.partial, data...)
	p.partial = nil

	var events []Event
	i := 0
	for i < len(buf) {
		n := p.tryParse(buf[i:], &events)
		if n == 0 {
			// Need more data; keep the remainder for the next feed.
			partial := make([]byte, len(buf)-i)
			copy(partial, buf[i:])
			p.partial = partial
			break
		}
		i += n
	}
	return events
}

// tryParse attempts to parse a single event from buf. Returns the number of
// bytes consumed, or 0 if more data is needed.
func (p *vtParser) tryParse(buf []byte, events *[]Event) int {
	b := buf[0]

	switch {
	case b == 0x1b: // ESC — start of escape sequence
		return p.parseEsc(buf, events)
	case b >= 0x20 && b <= 0x7e: // Printable ASCII
		*events = append(*events, KeyEvent{Key: keybind.NewRune(rune(b), 0)})
		return 1
	case b >= 0xc0 && b <= 0xdf: // UTF-8 2-byte lead
		if len(buf) < 2 {
			return 0
		}
		r := rune(b&0x1f)<<6 | rune(buf[1]&0x3f)
		*events = append(*events, KeyEvent{Key: keybind.NewRune(r, 0)})
		return 2
	case b >= 0xe0 && b <= 0xef: // UTF-8 3-byte lead
		if len(buf) < 3 {
			return 0
		}
		r := rune(b&0x0f)<<12 | rune(buf[1]&0x3f)<<6 | rune(buf[2]&0x3f)
		*events = append(*events, KeyEvent{Key: keybind.NewRune(r, 0)})
		return 3
	case b >= 0xf0 && b <= 0xf7: // UTF-8 4-byte lead
		if len(buf) < 4 {
			return 0
		}
		r := rune(b&0x07)<<18 | rune(buf[1]&0x3f)<<12 | rune(buf[2]&0x3f)<<6 | rune(buf[3]&0x3f)
		*events = append(*events, KeyEvent{Key: keybind.NewRune(r, 0)})
		return 4
	case b == 0x7f: // DEL → Backspace
		*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeBackspace, 0)})
		return 1
	case b < 0x20: // Control characters (0x00–0x1f, ESC handled above)
		*events = append(*events, ctrlKeyEvent(b))
		return 1
	default:
		return 1 // skip unknown byte
	}
}

// parseEsc handles an ESC-prefixed sequence.
func (p *vtParser) parseEsc(buf []byte, events *[]Event) int {
	if len(buf) < 2 {
		// In VT input mode, complete sequences are sent in one read.
		// A lone ESC byte means the user pressed Escape by itself.
		*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeEsc, 0)})
		return 1
	}

	switch buf[1] {
	case '[': // CSI sequence
		return p.parseCSI(buf, events)
	case 'O': // SS3 sequence
		return p.parseSS3(buf, events)
	case 0x1b: // ESC ESC → Alt+Escape
		*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeEsc, keybind.ModAlt)})
		return 2
	default:
		// ESC + char → Alt+char
		r := rune(buf[1])
		*events = append(*events, KeyEvent{Key: keybind.NewRune(r, keybind.ModAlt)})
		return 2
	}
}

// parseCSI handles CSI sequences: ESC [ ... final_byte
func (p *vtParser) parseCSI(buf []byte, events *[]Event) int {
	// buf[0] = ESC, buf[1] = '['
	i := 2

	// Check for SGR mouse: ESC [ <
	if i < len(buf) && buf[i] == '<' {
		return p.parseSGRMouse(buf, events)
	}

	// Scan for the final byte (0x40–0x7E).
	for i < len(buf) {
		b := buf[i]
		if b >= 0x40 && b <= 0x7e {
			i++
			paramStr := string(buf[2 : i-1])
			return p.dispatchCSI(events, paramStr, b, i)
		}
		if b == 0x1b {
			// Broken sequence; consume the ESC [ prefix.
			return 2
		}
		i++
	}

	return 0 // need more data — no final byte found
}

// dispatchCSI routes a complete CSI sequence to the appropriate handler.
func (p *vtParser) dispatchCSI(events *[]Event, params string, final byte, consumed int) int {
	// SGR mouse is handled before we get here.
	// Handle Shift+Tab: ESC [ Z
	if final == 'Z' && params == "" {
		*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeTab, keybind.ModShift)})
		return consumed
	}

	// Parse modifier parameter if present (format: "1;X" or just "1").
	mods := keybind.ModNone
	var paramNum int
	if params != "" {
		parts := strings.Split(params, ";")
		if len(parts) >= 1 {
			paramNum, _ = strconv.Atoi(parts[0])
		}
		if len(parts) >= 2 {
			modCode, _ := strconv.Atoi(parts[1])
			mods = vtModToKeybind(modCode)
		}
	}

	switch final {
	case 'A':
		*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeUp, mods)})
	case 'B':
		*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeDown, mods)})
	case 'C':
		*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeRight, mods)})
	case 'D':
		*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeLeft, mods)})
	case 'H':
		*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeHome, mods)})
	case 'F':
		*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeEnd, mods)})
	case '~':
		switch paramNum {
		case 2:
			*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeInsert, mods)})
		case 3:
			*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeDelete, mods)})
		case 5:
			*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodePageUp, mods)})
		case 6:
			*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodePageDown, mods)})
		case 15:
			*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeF5, mods)})
		case 17:
			*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeF6, mods)})
		case 18:
			*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeF7, mods)})
		case 19:
			*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeF8, mods)})
		case 20:
			*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeF9, mods)})
		case 21:
			*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeF10, mods)})
		case 23:
			*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeF11, mods)})
		case 24:
			*events = append(*events, KeyEvent{Key: keybind.NewCode(keybind.CodeF12, mods)})
		}
	}

	return consumed
}

// parseSS3 handles SS3 sequences: ESC O X  (F1–F4) and extended ESC O 1 ; X P.
func (p *vtParser) parseSS3(buf []byte, events *[]Event) int {
	if len(buf) < 3 {
		return 0 // need more data
	}

	// Extended SS3: ESC O 1 ; <mod> <letter>
	// The "1;" prefix indicates a modified key.
	if buf[2] >= '0' && buf[2] <= '9' {
		// Scan for the pattern: digits ; digits letter
		i := 2
		for i < len(buf) && buf[i] >= '0' && buf[i] <= '9' {
			i++
		}
		if i < len(buf) && buf[i] == ';' {
			i++ // skip ;
			modStart := i
			for i < len(buf) && buf[i] >= '0' && buf[i] <= '9' {
				i++
			}
			if i < len(buf) {
				final := buf[i]
				i++
				modCode, _ := strconv.Atoi(string(buf[modStart : i-1]))
				mods := vtModToKeybind(modCode)

				var code keybind.Code
				switch final {
				case 'P':
					code = keybind.CodeF1
				case 'Q':
					code = keybind.CodeF2
				case 'R':
					code = keybind.CodeF3
				case 'S':
					code = keybind.CodeF4
				default:
					return i
				}
				*events = append(*events, KeyEvent{Key: keybind.NewCode(code, mods)})
				return i
			}
		}
		// Malformed — fall through
	}

	// Simple SS3: ESC O <letter>
	final := buf[2]
	var code keybind.Code
	switch final {
	case 'P':
		code = keybind.CodeF1
	case 'Q':
		code = keybind.CodeF2
	case 'R':
		code = keybind.CodeF3
	case 'S':
		code = keybind.CodeF4
	default:
		return 3 // consume but ignore
	}

	*events = append(*events, KeyEvent{Key: keybind.NewCode(code, 0)})
	return 3
}

// parseSGRMouse handles ESC [ < Btn ; Col ; Row M/m  sequences.
func (p *vtParser) parseSGRMouse(buf []byte, events *[]Event) int {
	// buf[0] = ESC, buf[1] = '[', buf[2] = '<'
	i := 3
	// Scan for the final byte M or m.
	for i < len(buf) {
		if buf[i] == 'M' || buf[i] == 'm' {
			i++
			paramStr := string(buf[3 : i-1])
			parts := strings.Split(paramStr, ";")
			if len(parts) == 3 {
				btn, _ := strconv.Atoi(parts[0])
				col, _ := strconv.Atoi(parts[1])
				row, _ := strconv.Atoi(parts[2])
				release := buf[i-1] == 'm'

				// Log parsed SGR mouse sequence.
				consoleDebugLog("VT INPUT: SGR mouse parsed: btn=%d col=%d row=%d release=%v raw=%x", btn, col, row, release, buf[:i])

				evt := sgrMouseToEvent(btn, col, row, release)
				if evt != nil {
					*events = append(*events, *evt)
				}
			}
			return i
		}
		if buf[i] == 0x1b {
			return 3 // broken sequence
		}
		i++
	}
	return 0 // need more data
}

// sgrMouseToEvent converts an SGR mouse parameter tuple into a MouseEvent.
func sgrMouseToEvent(btn, col, row int, release bool) *MouseEvent {
	// Button encoding:
	// bits 0-1: button (0=left, 1=middle, 2=right, 3=release/no button)
	// bit 2 (4): shift
	// bit 3 (8): alt/meta
	// bit 4 (16): ctrl
	// bit 5 (32): motion (drag)
	// bit 6 (64): wheel
	// bit 7 (128): extra button (button 8+)

	var mods int
	if btn&4 != 0 {
		mods |= 4 // shift
	}
	if btn&8 != 0 {
		mods |= 8 // alt
	}
	if btn&16 != 0 {
		mods |= 16 // ctrl
	}

	isMotion := btn&32 != 0
	isWheel := btn&64 != 0
	baseBtn := btn & 3

	x := col - 1 // convert to 0-based
	y := row - 1

	if release {
		return &MouseEvent{
			Button:    MouseNone,
			X:         x,
			Y:         y,
			Modifiers: mods,
		}
	}

	if isWheel {
		// Wheel up = 64, wheel down = 65
		if baseBtn == 0 {
			return &MouseEvent{Button: MouseWheelUp, X: x, Y: y, Modifiers: mods}
		}
		return &MouseEvent{Button: MouseWheelDown, X: x, Y: y, Modifiers: mods}
	}

	var button MouseButton
	switch baseBtn {
	case 0:
		button = MouseLeft
	case 1:
		button = MouseMiddle
	case 2:
		button = MouseRight
	default:
		button = MouseNone
	}

	if isMotion {
		return &MouseEvent{Button: button, X: x, Y: y, Down: true, Drag: true, Modifiers: mods}
	}

	return &MouseEvent{Button: button, X: x, Y: y, Down: true, Modifiers: mods}
}

// ctrlKeyEvent converts a control character (0x00–0x1F) to a KeyEvent.
func ctrlKeyEvent(b byte) KeyEvent {
	switch b {
	case 0x08:
		return KeyEvent{Key: keybind.NewCode(keybind.CodeBackspace, 0)}
	case 0x09:
		return KeyEvent{Key: keybind.NewCode(keybind.CodeTab, 0)}
	case 0x0d:
		return KeyEvent{Key: keybind.NewCode(keybind.CodeEnter, 0)}
	case 0x1b:
		return KeyEvent{Key: keybind.NewCode(keybind.CodeEsc, 0)}
	default:
		if b <= 0x1a {
			// Ctrl+letter: 0x01=C-A, 0x02=C-B, … 0x1a=C-Z
			return KeyEvent{Key: keybind.NewRune(rune('a')+rune(b-1), keybind.ModCtrl)}
		}
		if b == 0x00 {
			// Ctrl+Space
			return KeyEvent{Key: keybind.NewRune(' ', keybind.ModCtrl)}
		}
		return KeyEvent{Key: keybind.NewRune(rune(b), 0)}
	}
}

// vtModToKeybind converts an xterm CSI modifier code (1-based) to keybind.Mods.
// 1=none, 2=shift, 3=alt, 4=ctrl, 5=shift+alt, 6=alt+ctrl, 7=shift+alt+ctrl.
func vtModToKeybind(code int) keybind.Mods {
	code--
	if code <= 0 {
		return keybind.ModNone
	}
	var m keybind.Mods
	if code&1 != 0 {
		m |= keybind.ModShift
	}
	if code&2 != 0 {
		m |= keybind.ModAlt
	}
	if code&4 != 0 {
		m |= keybind.ModCtrl
	}
	return m
}
