package app

import (
	"strings"
	"testing"
	"time"

	"vimterm/internal/emulator"
	"vimterm/internal/pty"
	"vimterm/internal/search"
)

// TestShellPipeline launches a real shell through the ConPTY, pipes its output
// into the emulator, and verifies the emitted text lands in the cell grid.
// ConPTY works headless, so this runs without a console window.
func TestShellPipeline(t *testing.T) {
	const cols, rows = 80, 24
	sess, err := pty.Spawn("powershell.exe", nil, cols, rows)
	if err != nil {
		t.Skipf("cannot spawn shell: %v", err)
	}
	defer func() {
		_ = sess.Kill()
		_ = sess.Close()
	}()

	emu := emulator.New(cols, rows)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				_, _ = emu.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	if _, err := sess.Write([]byte("Write-Output hello_from_vimterm\r")); err != nil {
		t.Fatalf("write to pty: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(gridText(emu, cols, rows), "hello_from_vimterm") {
			// Also verify resize does not crash the pipeline.
			if err := sess.Resize(100, 30); err != nil {
				t.Fatalf("resize pty: %v", err)
			}
			emu.Resize(100, 30)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("output not seen in emulator grid:\n%s", gridText(emu, cols, rows))
}

// TestScrollbackWithRealShell generates enough output to overflow the screen
// and verifies scrolled-off lines are readable through ScrollbackCell.
func TestScrollbackWithRealShell(t *testing.T) {
	const cols, rows = 60, 10
	sess, err := pty.Spawn("powershell.exe", nil, cols, rows)
	if err != nil {
		t.Skipf("cannot spawn shell: %v", err)
	}
	defer func() {
		_ = sess.Kill()
		_ = sess.Close()
	}()

	emu := emulator.New(cols, rows)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				_, _ = emu.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// Wait for the shell to come up (its prompt visible) before writing.
	deadline := time.Now().Add(20 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		if strings.Contains(gridText(emu, cols, rows), ">") {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("shell prompt did not appear; grid:\n%s", gridText(emu, cols, rows))
	}

	// ConPTY can drop input written while the child is still arming its
	// console input. Wait out the startup window and retry if needed.
	time.Sleep(500 * time.Millisecond)
	cmd := "1..60 | ForEach-Object { \"line $_\" }\r"
	for attempt := 0; attempt < 4 && time.Now().Before(deadline); attempt++ {
		if _, err := sess.Write([]byte(cmd)); err != nil {
			t.Fatalf("write to pty: %v", err)
		}
		wait := time.Now().Add(5 * time.Second)
		for time.Now().Before(wait) {
			if emu.ScrollbackLen() > 0 {
				for y := 0; y < emu.ScrollbackLen(); y++ {
					if lineText(emu, y) == "line 1" {
						return
					}
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	t.Fatalf("scrollback did not contain line 1; scrollbackLen=%d, grid:\n%s",
		emu.ScrollbackLen(), gridText(emu, cols, rows))
}

// TestSearchWithRealShell verifies the search package against real scrollback
// content produced by an actual shell.
func TestSearchWithRealShell(t *testing.T) {
	const cols, rows = 60, 10
	sess, err := pty.Spawn("powershell.exe", nil, cols, rows)
	if err != nil {
		t.Skipf("cannot spawn shell: %v", err)
	}
	defer func() {
		_ = sess.Kill()
		_ = sess.Close()
	}()

	emu := emulator.New(cols, rows)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				_, _ = emu.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	deadline := time.Now().Add(20 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		if strings.Contains(gridText(emu, cols, rows), ">") {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("shell prompt did not appear; grid:\n%s", gridText(emu, cols, rows))
	}

	// Same ConPTY input-drop protection as TestScrollbackWithRealShell.
	line := func(absLine int) []emulator.Cell {
		sb := emu.ScrollbackLen()
		if absLine < 0 || absLine >= sb+emu.Height() {
			return nil
		}
		var cells []emulator.Cell
		for x := 0; x < cols; x++ {
			if absLine < sb {
				cells = append(cells, emu.ScrollbackCell(x, absLine))
			} else {
				cells = append(cells, emu.Cell(x, absLine-sb))
			}
		}
		return cells
	}
	time.Sleep(500 * time.Millisecond)
	cmd := "1..40 | ForEach-Object { \"probe-$_\" }\r"
	s := search.New(line)
	for attempt := 0; attempt < 4 && time.Now().Before(deadline); attempt++ {
		if _, err := sess.Write([]byte(cmd)); err != nil {
			t.Fatalf("write to pty: %v", err)
		}
		wait := time.Now().Add(5 * time.Second)
		for time.Now().Before(wait) {
			s.SetQuery([]rune("probe-1"))
			if len(s.Matches()) >= 2 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if len(s.Matches()) >= 2 {
			break
		}
	}
	if len(s.Matches()) < 2 {
		t.Fatalf("search found %d matches, want >= 2 (probe-1 and probe-10..19)", len(s.Matches()))
	}

	// Highlight a known matching line and verify the cells are marked.
	if m, ok := s.Next(-1, -1); ok {
		row := make([]emulator.Cell, cols)
		if m.Line < emu.ScrollbackLen() {
			for x := 0; x < cols; x++ {
				row[x] = emu.ScrollbackCell(x, m.Line)
			}
		} else {
			for x := 0; x < cols; x++ {
				row[x] = emu.Cell(x, m.Line-emu.ScrollbackLen())
			}
		}
		s.Highlight(row, m.Line)
		marked := 0
		for _, c := range row {
			if c.Reverse {
				marked++
			}
		}
		if marked == 0 {
			t.Fatalf("no highlighted cells on matching line %d", m)
		}
	}
}

func lineText(emu emulator.Emulator, y int) string {
	var sb strings.Builder
	for x := 0; x < 60; x++ {
		sb.WriteString(emu.ScrollbackCell(x, y).Content)
	}
	return strings.TrimRight(sb.String(), " ")
}

func gridText(emu emulator.Emulator, w, h int) string {
	var sb strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sb.WriteString(emu.Cell(x, y).Content)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
