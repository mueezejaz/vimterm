package app

// Temporary bug-verification tests (to be removed after review).

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"vimterm/internal/config"
	"vimterm/internal/console"
	"vimterm/internal/emulator"
	"vimterm/internal/keybind"
	"vimterm/internal/macro"
	"vimterm/internal/mode"
	"vimterm/internal/screen"
	"vimterm/internal/search"
	"vimterm/internal/selection"
)

// realApp builds an App with the same geometry relationship the real app
// uses: the emulator gets hostRows-1 (status line takes one row).
func realApp(t *testing.T, cols, hostRows int, content string) *App {
	t.Helper()
	cfg := config.Default()
	leader, err := keybind.ParseLeader(cfg.General.Leader)
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]map[string]string{
		"normal": cfg.Keybindings.Normal,
		"insert": cfg.Keybindings.Insert,
		"visual": cfg.Keybindings.Visual,
	}
	keymaps, err := keybind.BuildKeymaps(bindings, leader)
	if err != nil {
		t.Fatal(err)
	}
	termRows := terminalRows(hostRows)
	emu := emulator.New(cols, termRows)
	if _, err := emu.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	a := &App{
		emu:        emu,
		mods:       mode.NewManager(),
		vp:         screen.New(termRows),
		engine:     keybind.NewEngine(),
		macro:      macro.New(),
		cfg:        cfg,
		screenCols: cols,
		screenRows: hostRows,
		done:       make(chan struct{}),
	}
	a.engine.SetKeymaps(keymaps)
	a.search = search.New(a.bufferLineCells)
	a.actions = a.actionMap()
	a.vp.SetMax(a.emu.ScrollbackLen())
	return a
}

// 1. Click on a visible line lands the cursor one line above it.
func TestVerifyMouseOffByOne(t *testing.T) {
	a := realApp(t, 40, 6, "AAA\r\nBBB\r\nCCC\r\n")
	// Screen rows: row0=AAA row1=BBB row2=CCC (bufBottom=4, offset=0).
	// Clicking host row 1 ("BBB") should put the cursor on line 1.
	a.handleMouse(console.MouseEvent{Button: console.MouseLeft, X: 0, Y: 1, Down: true})
	if a.cur.Line != 1 {
		t.Fatalf("click on row 1: cursor line = %d, want 1 (off-by-one confirmed)", a.cur.Line)
	}
}

// 2. Moving the mouse with no buttons pressed after a click starts a ghost
// selection.
func TestVerifyHoverDragSelects(t *testing.T) {
	a := realApp(t, 40, 6, "hello world\r\n")
	a.handleMouse(console.MouseEvent{Button: console.MouseLeft, X: 1, Y: 0, Down: true})
	// Plain mouse move, no button held (Windows reports mouseMoved with no
	// buttons): must NOT begin a selection.
	a.handleMouse(console.MouseEvent{Button: console.MouseNone, X: 5, Y: 0, Drag: true})
	if a.sel.Active {
		t.Fatal("hover move with no button started a selection (ghost selection confirmed)")
	}
}

// 3. Releasing the left button cancels an in-progress drag selection.
func TestVerifyReleaseCancelsSelection(t *testing.T) {
	a := realApp(t, 40, 6, "hello world\r\n")
	a.handleMouse(console.MouseEvent{Button: console.MouseLeft, X: 1, Y: 0, Down: true})
	a.handleMouse(console.MouseEvent{Button: console.MouseLeft, X: 5, Y: 0, Drag: true})
	if !a.sel.Active {
		t.Fatal("drag should select")
	}
	// Button release: Down=false, Drag=false.
	a.handleMouse(console.MouseEvent{Button: console.MouseLeft, X: 5, Y: 0})
	if !a.sel.Active {
		t.Fatal("releasing the mouse button cancelled the selection (confirmed)")
	}
}

// 4. f{digit}: the digit must become the find target, not a count.
func TestVerifyFindDigit(t *testing.T) {
	a := realApp(t, 40, 6, "ab3cd3ef\r\n")
	a.cur = selection.Pos{Line: 0, Col: 0}
	a.curValid = true
	a.handleKey(keybind.Key{Code: keybind.CodeRune, Rune: 'f'})
	a.handleKey(keybind.Key{Code: keybind.CodeRune, Rune: '3'})
	if a.find.pending() {
		t.Fatalf("f then '3' left the find pending; cursor col=%d count=%d (digit hijacked as count)", a.cur.Col, a.count)
	}
	if a.cur.Col != 2 {
		t.Fatalf("cursor col = %d, want 2 (the '3')", a.cur.Col)
	}
}

// 5. Search scan stops at the first empty line.
func TestVerifySearchBlankLine(t *testing.T) {
	a := realApp(t, 40, 6, "alpha\r\n\r\nomega\r\n")
	for l := 0; l < 6; l++ {
		t.Logf("bufferLine(%d) len=%d %q", l, len(a.bufferLine(l)), string(a.bufferLine(l)))
	}
	a.search.SetQuery([]rune("omega"))
	if len(a.search.Matches()) == 0 {
		t.Fatal("search found no match for \"omega\" past the blank line (confirmed)")
	}
}

// 6. Cancelling a command prompt resets the viewport.
func TestVerifyCommandCancelResetsViewport(t *testing.T) {
	a := realApp(t, 40, 30, strings.Repeat("line\r\n", 40))
	a.vp.SetMax(a.emu.ScrollbackLen())
	a.vp.MoveUp(5)
	off := a.vp.Offset()
	if off == 0 {
		t.Fatal("expected to be scrolled up")
	}
	a.openCommand()
	a.handlePromptKey(keybind.Key{Code: keybind.CodeEsc})
	if a.vp.Offset() != off {
		t.Fatalf("cancelling ':' prompt jumped viewport %d -> %d (confirmed)", off, a.vp.Offset())
	}
}

// 7. Line-wise yank includes the full padded width (trailing spaces).
func TestVerifyYankTrailingSpaces(t *testing.T) {
	a := realApp(t, 20, 6, "hello\r\n")
	a.sel.Begin(selection.Pos{Line: 0, Col: 0})
	a.sel.SetLineWise(true)
	a.sel.Move(selection.Pos{Line: 0, Col: 0})
	text := a.sel.Text(a.bufferLine)
	t.Logf("linewise yank text = %q", text)
	if text != "hello" {
		t.Fatalf("yank = %q (%d trailing spaces confirmed)", text, len(text)-len("hello"))
	}
}

// 8. :clear leaves stale cursor coordinates (abs lines shift by the removed
// scrollback, but cur keeps its old value and now points at other content).
func TestVerifyClearCursorStale(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		sb.WriteString("L")
		sb.WriteString(itoa(i))
		sb.WriteString("\r\n")
	}
	a := realApp(t, 40, 30, sb.String())
	sblen := a.emu.ScrollbackLen()
	// Cursor on a screen row that holds content.
	cur := sblen + 10
	a.cur = selection.Pos{Line: cur, Col: 0}
	a.curValid = true
	a.execCommand("clear")
	if a.emu.ScrollbackLen() != 0 {
		t.Fatalf("scrollback after clear: %d", a.emu.ScrollbackLen())
	}
	if !a.curValid {
		t.Fatalf("cursor not re-derived after :clear (stale abs coords confirmed)")
	}
	if a.cur.Line == cur {
		t.Fatalf("cursor still at %d after :clear (stale abs coords confirmed)", cur)
	}
}

// 9. mergeAllowed treats an explicit black background as "no background":
// an nvim status line with bg #000000 is not merged in auto mode.
func TestVerifyMergeAllowedBlack(t *testing.T) {
	row := []emulator.Cell{
		{Content: "-", Width: 1, Bg: emulator.Color{R: 0, G: 0, B: 0}},
	}
	if !mergeAllowed(row, "auto") {
		t.Fatal("row with explicit black bg should count as a status line (zero-value conflation confirmed)")
	}
}

// 10. Data race / cross-session write: startReader captures the session but
// reads a.emu live, so a restarted shell's output goes to the OLD reader's
// current a.emu and races restartShell's replacement.
func TestVerifyReaderEmulatorRace(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	blocker := make(chan struct{})
	a.sess = &stuckSession{release: blocker}
	a.startReader()
	// While the old reader is parked in Read, swap the emulator like
	// restartShell does; then let the old session deliver output.
	old := a.emu
	a.emu = emulator.New(40, 5)
	close(blocker)
	// Wait for the old reader to deliver its output.
	for i := 0; i < 100; i++ {
		if strings.Contains(a.emu.Cell(0, 1).Content, "Z") || strings.Contains(old.Cell(0, 1).Content, "Z") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	newCell := a.emu.Cell(0, 1).Content
	oldCell := old.Cell(0, 1).Content
	t.Logf("after swap: new emu(0,1)=%q old emu(0,1)=%q", newCell, oldCell)
	if strings.Contains(newCell, "Z") {
		t.Fatal("old session output landed in the NEW emulator (confirmed)")
	}
	if !strings.Contains(oldCell, "Z") {
		t.Fatal("old session output was dropped entirely")
	}
}

type stuckSession struct {
	release chan struct{}
	written bool
}

func (s *stuckSession) Write(p []byte) (int, error) { return len(p), nil }
func (s *stuckSession) Read(p []byte) (int, error) {
	<-s.release
	if s.written {
		return 0, io.EOF
	}
	s.written = true
	copy(p, "ZZZZ\r\n")
	return 6, nil
}
func (s *stuckSession) Resize(int, int) error          { return nil }
func (s *stuckSession) Kill() error                    { return nil }
func (s *stuckSession) Close() error                   { return nil }
func (s *stuckSession) Name() string                   { return "stuck" }
func (s *stuckSession) Wait(ctx context.Context) error { return nil }
