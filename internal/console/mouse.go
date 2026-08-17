package console

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
	Button MouseButton
	X, Y   int // 0-based cell coordinates
	Down   bool // true for a press, false for a release
	Drag   bool // true when the mouse moved with a button held
	Double bool // true for a double click
}

func (MouseEvent) isEvent() {}

// MOUSE_EVENT_RECORD event flags.
const (
	mouseMoved   = 0x0001
	mouseDouble  = 0x0002
	mouseWheeled = 0x0004
	mouseHWheel  = 0x0008
)

// MOUSE_EVENT_RECORD button states.
const (
	leftButton   = 0x0001
	rightButton  = 0x0002
	middleButton = 0x0004
)

// mouseEventRecord mirrors MOUSE_EVENT_RECORD (16 bytes).
type mouseEventRecord struct {
	pos      windowsCoord
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
// reports the buttons held during a drag.
func mouseFromRecord(rec *mouseEventRecord) MouseEvent {
	x, y := int(rec.pos.x), int(rec.pos.y)
	flags := rec.eventFlags
	state := rec.buttonState & 0xffff

	if flags&mouseWheeled != 0 {
		delta := int16(rec.buttonState >> 16)
		if delta > 0 {
			return MouseEvent{Button: MouseWheelUp, X: x, Y: y}
		}
		return MouseEvent{Button: MouseWheelDown, X: x, Y: y}
	}

	down := state&leftButton != 0
	switch {
	case down && flags&mouseMoved != 0:
		return MouseEvent{Button: MouseLeft, X: x, Y: y, Drag: true}
	case down && flags&mouseDouble != 0:
		return MouseEvent{Button: MouseLeft, X: x, Y: y, Down: true, Double: true}
	case down:
		return MouseEvent{Button: MouseLeft, X: x, Y: y, Down: true}
	case state&rightButton != 0:
		return MouseEvent{Button: MouseRight, X: x, Y: y, Down: true}
	case state&middleButton != 0:
		return MouseEvent{Button: MouseMiddle, X: x, Y: y, Down: true}
	case flags&mouseMoved != 0:
		return MouseEvent{Button: MouseNone, X: x, Y: y, Drag: true}
	}
	return MouseEvent{Button: MouseLeft, X: x, Y: y}
}