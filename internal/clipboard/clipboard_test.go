package clipboard

import "testing"

// TestRoundTrip sets text on the clipboard and reads it back, preserving the
// previous clipboard contents.
func TestRoundTrip(t *testing.T) {
	prev, prevErr := GetText()

	text := "vimterm yank test: αβγ 你好 ✓"
	if err := SetText(text); err != nil {
		t.Fatalf("SetText: %v", err)
	}
	got, err := GetText()
	if err != nil {
		t.Fatalf("GetText: %v", err)
	}
	if got != text {
		t.Fatalf("round-trip mismatch: %q != %q", got, text)
	}

	// Restore the previous contents (if any).
	if prevErr == nil {
		if err := SetText(prev); err != nil {
			t.Logf("could not restore previous clipboard: %v", err)
		}
	} else if err := SetText(""); err == nil {
		// Leave the test text in place; cannot restore an unknown prior state.
		t.Log("previous clipboard was empty; left test text in place")
	}
}
