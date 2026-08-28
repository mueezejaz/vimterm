package console

import "testing"

func TestMouseFromRecord(t *testing.T) {
	cases := []struct {
		name     string
		rec      mouseEventRecord
		prevBtn  uint32
		wantBtn  MouseButton
		wantDown bool
		wantDrag bool
		wantDoub bool
	}{
		{"left press",
			mouseEventRecord{pos: windowsCoord{x: 3, y: 2}, buttonState: leftButton},
			0,
			MouseLeft, true, false, false},
		{"left release",
			mouseEventRecord{pos: windowsCoord{x: 3, y: 2}},
			leftButton,
			MouseLeft, false, false, false},
		{"drag",
			mouseEventRecord{pos: windowsCoord{x: 5, y: 2}, buttonState: leftButton, eventFlags: mouseMoved},
			leftButton,
			MouseLeft, true, true, false},
		{"double click",
			mouseEventRecord{pos: windowsCoord{x: 3, y: 2}, buttonState: leftButton, eventFlags: mouseDouble},
			0,
			MouseLeft, true, false, true},
		{"right press",
			mouseEventRecord{pos: windowsCoord{x: 3, y: 2}, buttonState: rightButton},
			0,
			MouseRight, true, false, false},
		{"wheel up",
			mouseEventRecord{pos: windowsCoord{x: 3, y: 2}, buttonState: 0x0078_0000, eventFlags: mouseWheeled},
			0,
			MouseWheelUp, false, false, false},
		{"wheel down",
			mouseEventRecord{pos: windowsCoord{x: 3, y: 2}, buttonState: 0xff88_0000, eventFlags: mouseWheeled},
			0,
			MouseWheelDown, false, false, false},
	}
	for _, c := range cases {
		got := mouseFromRecord(&c.rec, c.prevBtn)
		if got.Button != c.wantBtn || got.Down != c.wantDown || got.Drag != c.wantDrag || got.Double != c.wantDoub {
			t.Errorf("%s: got %+v", c.name, got)
		}
		if c.name == "left press" && (got.X != 3 || got.Y != 2) {
			t.Errorf("%s: coords = %d,%d", c.name, got.X, got.Y)
		}
	}
}
