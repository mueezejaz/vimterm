package app

import (
	"strings"
	"testing"
	"time"

	"vimterm/internal/config"
	"vimterm/internal/emulator"
	"vimterm/internal/keybind"
	"vimterm/internal/macro"
	"vimterm/internal/mode"
	"vimterm/internal/pty"
	"vimterm/internal/screen"
	"vimterm/internal/search"
)

// newMotionApp builds an App with real engine, actions, emulator and
// viewport but no console/session, so key routing can be exercised headless.
func newMotionApp(t *testing.T, cols, rows int, content string) *App {
	t.Helper()
	cfg := config.Default()
	leader, err := keybind.ParseLeader(cfg.General.Leader)
	if err != nil {
		t.Fatal(err)
	}
	tables := cfg.Keybindings.ActionTables()
	bindings := map[string]map[string][]string{
		"normal": tables["normal"],
		"insert": tables["insert"],
		"visual": tables["visual"],
	}
	keymaps, err := keybind.BuildKeymaps(bindings, leader)
	if err != nil {
		t.Fatal(err)
	}
	emu := emulator.New(cols, rows)
	if _, err := emu.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	a := &App{
		emu:    emu,
		mods:   mode.NewManager(),
		vp:     screen.New(rows),
		engine: keybind.NewEngine(),
		macro:  macro.New(),
	}
	a.engine.SetKeymaps(keymaps)
	// The production default is 1000ms; a plain 1000 here would be a
	// 1000ns timeout that flushes pending sequences (gg, dw, ...) whenever
	// the scheduler delays the next key by more than a microsecond.
	a.engine.SetTimeout(1000 * time.Millisecond)
	a.search = search.New(a.bufferLineCells)
	a.actions = a.actionMap()
	// renderFrame calls SetMax before any motion in the real app.
	a.vp.SetMax(a.emu.ScrollbackLen())
	return a
}

func writeLines(sb *strings.Builder, n int) {
	for i := 0; i < n; i++ {
		sb.WriteString("line ")
		sb.WriteString(itoa(i))
		sb.WriteString("\r\n")
	}
}

func press(t *testing.T, a *App, key keybind.Key) {
	t.Helper()
	a.handleKey(key)
}

func TestMotionInNormalMode(t *testing.T) {
	var sb strings.Builder
	writeLines(&sb, 30)
	a := newMotionApp(t, 40, 5, sb.String())

	press(t, a, keybind.NewRune('g', 0))
	press(t, a, keybind.NewRune('g', 0))
	start := a.cur.Line
	if start != 0 {
		t.Fatalf("gg: cur.Line = %d, want 0", start)
	}
	press(t, a, keybind.NewRune('j', 0))
	if a.cur.Line != start+1 {
		t.Fatalf("j: cur.Line = %d, want %d", a.cur.Line, start+1)
	}
	press(t, a, keybind.NewRune('k', 0))
	if a.cur.Line != start {
		t.Fatalf("k: cur.Line = %d, want %d", a.cur.Line, start)
	}
	press(t, a, keybind.NewRune('h', 0))
	if a.cur.Col != 0 {
		t.Fatalf("h: cur.Col = %d, want 0 (clamped)", a.cur.Col)
	}
	press(t, a, keybind.NewRune('l', 0))
	if a.cur.Col != 1 {
		t.Fatalf("l: cur.Col = %d, want 1", a.cur.Col)
	}
	if !a.curValid {
		t.Fatal("cursor must be valid after motion")
	}
}

func TestMotionArrowKeys(t *testing.T) {
	var sb strings.Builder
	writeLines(&sb, 30)
	a := newMotionApp(t, 40, 5, sb.String())

	press(t, a, keybind.NewRune('g', 0))
	press(t, a, keybind.NewRune('g', 0))
	press(t, a, keybind.NewCode(keybind.CodeDown, 0))
	if a.cur.Line != 1 {
		t.Fatalf("down arrow: cur.Line = %d, want 1", a.cur.Line)
	}
	press(t, a, keybind.NewCode(keybind.CodeUp, 0))
	if a.cur.Line != 0 {
		t.Fatalf("up arrow: cur.Line = %d, want 0", a.cur.Line)
	}
	press(t, a, keybind.NewCode(keybind.CodeRight, 0))
	if a.cur.Col != 1 {
		t.Fatalf("right arrow: cur.Col = %d, want 1", a.cur.Col)
	}
	press(t, a, keybind.NewCode(keybind.CodeLeft, 0))
	if a.cur.Col != 0 {
		t.Fatalf("left arrow: cur.Col = %d, want 0", a.cur.Col)
	}
}

func TestMotionGotoTopBottom(t *testing.T) {
	var sb strings.Builder
	writeLines(&sb, 30)
	a := newMotionApp(t, 40, 5, sb.String())

	press(t, a, keybind.NewRune('g', 0))
	press(t, a, keybind.NewRune('g', 0))
	if a.cur.Line != 0 {
		t.Fatalf("gg: cur.Line = %d, want 0", a.cur.Line)
	}
	if a.vp.Offset() == 0 {
		t.Fatal("gg: viewport must be scrolled to the top")
	}

	press(t, a, keybind.NewRune('G', keybind.ModShift))
	want := a.emu.ScrollbackLen() + a.emu.Height() - 1
	if a.cur.Line != want {
		t.Fatalf("G: cur.Line = %d, want %d", a.cur.Line, want)
	}
	if a.vp.Offset() != 0 {
		t.Fatalf("G: offset = %d, want 0", a.vp.Offset())
	}
}

func TestMotionCappedAtBufferBounds(t *testing.T) {
	var sb strings.Builder
	writeLines(&sb, 3)
	a := newMotionApp(t, 40, 5, sb.String())

	// Jump far past the end of the buffer with repeated j presses.
	for i := 0; i < 100; i++ {
		press(t, a, keybind.NewRune('j', 0))
	}
	want := a.emu.ScrollbackLen() + a.emu.Height() - 1
	if a.cur.Line != want {
		t.Fatalf("j x100: cur.Line = %d, want %d", a.cur.Line, want)
	}

	for i := 0; i < 100; i++ {
		press(t, a, keybind.NewRune('k', 0))
	}
	if a.cur.Line != 0 {
		t.Fatalf("k x100: cur.Line = %d, want 0", a.cur.Line)
	}
}

// visible asserts the virtual cursor lies inside the current viewport.
func visible(a *App) bool {
	if !a.curValid {
		return false
	}
	top := a.topAbsLine()
	rows := a.emu.Height()
	return a.cur.Line >= top && a.cur.Line <= top+rows-1
}

// TestMotionWithOutputGrowthKeepsCursorVisible reproduces the disappearing
// cursor: scrollback grows between frames (new shell output) while the
// viewport max is stale, and a motion must still keep the cursor visible.
func TestMotionWithOutputGrowthKeepsCursorVisible(t *testing.T) {
	var sb strings.Builder
	writeLines(&sb, 20)
	a := newMotionApp(t, 40, 5, sb.String())

	press(t, a, keybind.NewRune('g', 0))
	press(t, a, keybind.NewRune('g', 0))
	if !visible(a) {
		t.Fatal("cursor must be visible after gg")
	}

	// New output arrives (the reader goroutine feeds the emulator); the
	// viewport max is refreshed only at the next render.
	var more strings.Builder
	writeLines(&more, 10)
	if _, err := a.emu.Write([]byte(more.String())); err != nil {
		t.Fatal(err)
	}

	press(t, a, keybind.NewRune('k', 0))
	if a.cur.Line != 0 {
		t.Fatalf("k: cur.Line = %d, want 0", a.cur.Line)
	}
	if !visible(a) {
		t.Fatalf("cursor disappeared after motion with growing scrollback: cur=%+v top=%d rows=%d",
			a.cur, a.topAbsLine(), a.emu.Height())
	}

	press(t, a, keybind.NewRune('j', 0))
	if !visible(a) {
		t.Fatalf("cursor disappeared after j: cur=%+v top=%d", a.cur, a.topAbsLine())
	}
}

func TestMotionIgnoresInsertMode(t *testing.T) {
	var sb strings.Builder
	writeLines(&sb, 30)
	a := newMotionApp(t, 40, 5, sb.String())

	sess, err := pty.Spawn("powershell.exe", nil, 40, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Kill()
	defer sess.Close()
	a.sess = sess

	press(t, a, keybind.NewRune('g', 0))
	press(t, a, keybind.NewRune('g', 0))
	press(t, a, keybind.NewRune('i', 0))
	if !a.mods.Is(mode.ModeInsert) {
		t.Fatal("i must enter insert mode")
	}
	line := a.cur.Line
	press(t, a, keybind.NewRune('j', 0))
	press(t, a, keybind.NewRune('k', 0))
	press(t, a, keybind.NewRune('h', 0))
	if a.cur.Line != line {
		t.Fatalf("motion in insert changed cur: %d -> %d", line, a.cur.Line)
	}
}
