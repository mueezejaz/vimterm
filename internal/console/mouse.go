package console

import "fmt"

// MouseButton identifies the button that produced a mouse event.
type MouseButton int

const (
	MouseNone MouseButton = iota
	MouseLeft
	MouseRight
	MouseMiddle
	MouseWheelUp
	MouseWheelDown
)

// MouseEvent reports a mouse press, release, drag or wheel action.
type MouseEvent struct {
	Button    MouseButton
	X, Y      int  // 0-based cell coordinates
	Down      bool // true for a press, false for a release
	Drag      bool // true when the mouse moved with a button held
	Double    bool // true for a double click
	Modifiers int  // shift=4, alt=8, ctrl=16 (dwControlKeyState bits)
}

func (MouseEvent) isEvent() {}

// MOUSE_EVENT_RECORD event flags.
const (
	mouseMoved   = 0x0001
	mouseDouble  = 0x0002
	mouseWheeled = 0x0004
)

// MOUSE_EVENT_RECORD button states.
const (
	leftButton   = 0x0001
	rightButton  = 0x0002
	middleButton = 0x0004
)

// dwControlKeyState bits for modifier keys.
const (
	keyShift = 0x0010
	keyAlt   = 0x0002
	keyCtrl  = 0x0008
)

// mouseEventRecord mirrors MOUSE_EVENT_RECORD (16 bytes).
type mouseEventRecord struct {
	pos             windowsCoord
	buttonState     uint32
	controlKeyState uint32
	eventFlags      uint32
}

// windowsCoord mirrors the Win32 COORD structure.
type windowsCoord struct {
	x, y int16
}

// mouseFromRecord translates a raw MOUSE_EVENT_RECORD into a MouseEvent.
// Wheel deltas live in the high word of buttonState; the low word still
// reports the buttons held during a drag. prevBtnState is the buttonState
// from the previous mouse event, used to identify which button was released.
func mouseFromRecord(rec *mouseEventRecord, prevBtnState uint32) MouseEvent {
	x, y := int(rec.pos.x), int(rec.pos.y)
	flags := rec.eventFlags
	state := rec.buttonState & 0xffff

	var mods int
	if rec.controlKeyState&keyShift != 0 {
		mods |= 4
	}
	if rec.controlKeyState&keyAlt != 0 {
		mods |= 8
	}
	if rec.controlKeyState&keyCtrl != 0 {
		mods |= 16
	}

	if flags&mouseWheeled != 0 {
		delta := int16(rec.buttonState >> 16)
		if delta > 0 {
			return MouseEvent{Button: MouseWheelUp, X: x, Y: y, Modifiers: mods}
		}
		return MouseEvent{Button: MouseWheelDown, X: x, Y: y, Modifiers: mods}
	}

	down := state&leftButton != 0
	switch {
	case down && flags&mouseMoved != 0:
		return MouseEvent{Button: MouseLeft, X: x, Y: y, Down: true, Drag: true, Modifiers: mods}
	case down && flags&mouseDouble != 0:
		return MouseEvent{Button: MouseLeft, X: x, Y: y, Down: true, Double: true, Modifiers: mods}
	case down:
		return MouseEvent{Button: MouseLeft, X: x, Y: y, Down: true, Modifiers: mods}
	case state&rightButton != 0:
		return MouseEvent{Button: MouseRight, X: x, Y: y, Down: true, Modifiers: mods}
	case state&middleButton != 0:
		return MouseEvent{Button: MouseMiddle, X: x, Y: y, Down: true, Modifiers: mods}
	case flags&mouseMoved != 0:
		return MouseEvent{Button: MouseNone, X: x, Y: y, Drag: true, Modifiers: mods}
	}

	// Button release: determine which button was released from previous state.
	released := prevBtnState & 0xffff
	switch {
	case released&leftButton != 0:
		return MouseEvent{Button: MouseLeft, X: x, Y: y, Modifiers: mods}
	case released&rightButton != 0:
		return MouseEvent{Button: MouseRight, X: x, Y: y, Modifiers: mods}
	case released&middleButton != 0:
		return MouseEvent{Button: MouseMiddle, X: x, Y: y, Modifiers: mods}
	}
	return MouseEvent{Button: MouseNone, X: x, Y: y, Modifiers: mods}
}

// MouseToVT encodes a MouseEvent as a VT SGR mouse escape sequence.
//
//	SGR press:   ESC [ < Cb ; Cx ; Cy M
//	SGR release: ESC [ < Cb ; Cx ; Cy m
//
// The button byte (Cb) follows the xterm SGR encoding:
//   - bits 0-1: button (0=left, 1=middle, 2=right, 3=release)
//   - bit 2 (4): shift
//   - bit 3 (8): alt/meta
//   - bit 4 (16): ctrl
//   - bit 5 (32): motion (drag)
//   - bit 6 (64): wheel
func MouseToVT(e MouseEvent) []byte {
	var btn int
	release := false

	switch e.Button {
	case MouseLeft:
		btn = 0
		release = !e.Down
	case MouseMiddle:
		btn = 1
		release = !e.Down
	case MouseRight:
		btn = 2
		release = !e.Down
	case MouseWheelUp:
		btn = 64
	case MouseWheelDown:
		btn = 65
	case MouseNone:
		if e.Drag {
			btn = 3 | 32 // motion with no button = button 3 + motion bit
		} else {
			btn = 3 // release
			release = true
		}
	default:
		return nil
	}

	if e.Drag && e.Button != MouseNone && e.Button != MouseWheelUp && e.Button != MouseWheelDown {
		btn |= 32
	}

	btn |= e.Modifiers

	term := 'M'
	if release {
		term = 'm'
	}

	// Clamp coordinates to 1-based positive values.
	x := e.X + 1
	y := e.Y + 1
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}

	return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", btn, x, y, term))
}
