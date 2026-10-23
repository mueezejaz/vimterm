package search

import (
	"testing"

	"vimterm/internal/emulator"
)

// benchLine returns a Line of n identical rows of ordinary shell output. The
// row is prebuilt so the benchmark measures the scan itself, not the
// provider's allocations (the real app's provider reuses a scratch buffer
// too).
func benchLine(n int, text string) Line {
	row := make([]emulator.Cell, 0, len(text))
	for _, r := range text {
		row = append(row, emulator.Cell{Content: string(r), Width: 1})
	}
	return func(l int) []emulator.Cell {
		if l < 0 || l >= n {
			return nil
		}
		return row
	}
}

// BenchmarkRecompute measures a full-buffer rescan, which is what every
// keystroke of the incremental search prompt pays on a large scrollback.
func BenchmarkRecompute(b *testing.B) {
	s := New(benchLine(5000, "the quick brown fox jumps over the lazy dog"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.SetQueryGen([]rune("fox"), i)
	}
}

// BenchmarkHighlight measures the per-visible-row highlight pass, paid for
// every screen row on every frame while a search is active.
func BenchmarkHighlight(b *testing.B) {
	s := New(benchLine(1, "the quick brown fox jumps over the lazy dog"))
	s.SetQuery([]rune("the"))
	row := make([]emulator.Cell, 54)
	for x := range row {
		row[x] = emulator.Cell{Content: string(rune('a' + x%26)), Width: 1}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Highlight(row, 0, emulator.Color{}, true)
	}
}
