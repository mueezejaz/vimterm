package console

// Regression tests for auto-repeat: Windows coalesces held keys into one
// INPUT_RECORD with wRepeatCount > 1, which used to be dropped, so holding
// a key produced a single press.

import (
	"testing"

	"vimterm/internal/keybind"
)

func TestKeyEventsFromRecordRepeats(t *testing.T) {
	tests := []struct {
		name   string
		vk     uint16
		r      rune
		state  uint32
		repeat uint16
		want   int
	}{
		{"single press", 'A', 'a', 0, 1, 1},
		{"held key", 'A', 'a', 0, 7, 7},
		{"zero count treated as one", 'A', 'a', 0, 0, 1},
		{
			"held special key",
			vkDown, 0, 0,
			12,
			12,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := keyEventsFromRecord(tt.vk, tt.r, tt.state, tt.repeat)
			if len(keys) != tt.want {
				t.Fatalf("got %d events, want %d", len(keys), tt.want)
			}
			for i, k := range keys {
				if k != keys[0] {
					t.Fatalf("event %d = %+v, want %+v", i, k, keys[0])
				}
			}
			if len(keys) > 0 && tt.vk == vkDown && keys[0].Code != keybind.CodeDown {
				t.Fatalf("special key code = %v, want CodeDown", keys[0].Code)
			}
		})
	}
}

func TestKeyEventsFromRecordDropsInvalid(t *testing.T) {
	// vk without a rune and not a known code maps to the zero Key; it must
	// produce no events rather than one zero event.
	if keys := keyEventsFromRecord(0xFF, 0, 0, 3); len(keys) != 0 {
		t.Fatalf("invalid record produced %d events, want 0", len(keys))
	}
}
