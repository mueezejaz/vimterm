package screen

import "testing"

func TestViewportBasics(t *testing.T) {
	v := New(24)
	if v.Offset() != 0 || v.ScrolledUp() {
		t.Fatal("viewport must start at live position")
	}
	v.SetMax(100)
	v.MoveUp(3)
	if v.Offset() != 3 || !v.ScrolledUp() {
		t.Fatalf("offset = %d, scrolled = %v", v.Offset(), v.ScrolledUp())
	}
	v.MoveDown(1)
	if v.Offset() != 2 {
		t.Fatalf("offset after down = %d, want 2", v.Offset())
	}
	v.MoveDown(99)
	if v.Offset() != 0 {
		t.Fatalf("offset clamps to 0, got %d", v.Offset())
	}
	v.MoveUp(1000)
	if v.Offset() != 100 {
		t.Fatalf("offset clamps to max 100, got %d", v.Offset())
	}
}

func TestViewportPages(t *testing.T) {
	v := New(24)
	v.SetMax(200)
	v.PageUp()
	if v.Offset() != 12 {
		t.Fatalf("PageUp = %d, want 12 (half of 24)", v.Offset())
	}
	v.PageDown()
	if v.Offset() != 0 {
		t.Fatalf("PageDown back to 0, got %d", v.Offset())
	}
}

func TestViewportJumps(t *testing.T) {
	v := New(24)
	v.SetMax(80)
	v.GotoTop()
	if v.Offset() != 80 {
		t.Fatalf("GotoTop = %d, want 80", v.Offset())
	}
	v.GotoBottom()
	if v.Offset() != 0 {
		t.Fatalf("GotoBottom = %d, want 0", v.Offset())
	}
}

func TestViewportMaxShrinks(t *testing.T) {
	v := New(24)
	v.SetMax(50)
	v.GotoTop()
	v.SetMax(10)
	if v.Offset() != 10 {
		t.Fatalf("offset must clamp when max shrinks, got %d", v.Offset())
	}
}

func TestViewportRowsChange(t *testing.T) {
	v := New(24)
	v.SetMax(100)
	v.SetRows(30)
	v.PageUp()
	if v.Offset() != 15 {
		t.Fatalf("PageUp after SetRows(30) = %d, want 15", v.Offset())
	}
}