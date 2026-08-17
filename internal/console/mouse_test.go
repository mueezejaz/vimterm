package console

import "testing"

func TestMouseFromRecord(t *testing.T) {
	cases := []struct {
		name     string
		rec      mouseEventRecord
		wantBtn  MouseButton
		wantDown bool
		wantDrag bool
		wantDoub bool
	}{
		{"left press",
			mouseEventRecord{pos: windowsCoord{x: 3, y: 2}, buttonState: leftButton},
			MouseLeft, true, false, false},
		{"left release",
			mouseEventRecord{pos: windowsCoord{x: 3, y: 2}},
			MouseLeft, false, false, false},
		{"drag",
			mouseEventRecord{pos: windowsCoord{x: 5, y: 2}, buttonState: leftButton, eventFlags: mouseMoved},
			MouseLeft, false, true, false},
		{"double click",
			mouseEventRecord{pos: windowsCoord{x: 3, y: 2}, buttonState: leftButton, eventFlags: mouseDouble},
			MouseLeft, true, false, true},
		{"right press",
			mouseEventRecord{pos: windowsCoord{x: 3, y: 2}, buttonState: rightButton},
			MouseRight, true, false, false},
		{"wheel up",
			mouseEventRecord{pos: windowsCoord{x: 3, y: 2}, buttonState: 0x0078_0000, eventFlags: mouseWheeled},
			MouseWheelUp, false, false, false},
		{"wheel down",
			mouseEventRecord{pos: windowsCoord{x: 3, y: 2}, buttonState: 0xff88_0000, eventFlags: mouseWheeled},
			MouseWheelDown, false, false, false},
	}
	for _, c := range cases {
		got := mouseFromRecord(&c.rec)
		if got.Button != c.wantBtn || got.Down != c.wantDown || got.Drag != c.wantDrag || got.Double != c.wantDoub {
			t.Errorf("%s: got %+v", c.name, got)
		}
		if c.name == "left press" && (got.X != 3 || got.Y != 2) {
			t.Errorf("%s: coords = %d,%d", c.name, got.X, got.Y)
		}
	}
}