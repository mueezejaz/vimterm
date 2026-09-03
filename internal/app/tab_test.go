package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"vimterm/internal/config"
	"vimterm/internal/cursortrail"
	"vimterm/internal/emulator"
	"vimterm/internal/keybind"
	"vimterm/internal/macro"
	"vimterm/internal/mode"
	"vimterm/internal/search"
)

// newTabTestApp builds a headless App focused on tab 0, plus n-1 extra tabs
// whose buffers contain "tab<i>". It mirrors newApp's wiring without a
// console. Sessions are the shared fakeSession from paste_test.go.
func newTabTestApp(t *testing.T, n int) *App {
	t.Helper()
	cfg := config.Default()
	a := &App{
		cfg:        cfg,
		mods:       mode.NewManager(),
		engine:     keybind.NewEngine(),
		macro:      macro.New(),
		quit:       make(chan struct{}),
		screenCols: 40,
		screenRows: 10,
		curBlink:   true,
		lastInput:  time.Now(),
		trail:      cursortrail.New(cursortrail.DefaultConfig()),
	}
	leader, err := keybind.ParseLeader(cfg.General.Leader)
	if err != nil {
		t.Fatal(err)
	}
	keymaps, err := keybind.BuildKeymaps(cfg.Keybindings.ActionTables(), leader)
	if err != nil {
		t.Fatal(err)
	}
	a.engine.SetKeymaps(keymaps)
	a.engine.SetTimeout(time.Second)
	a.actions = a.actionMap()
	for i := 0; i < n; i++ {
		emu := emulator.New(40, 9)
		if _, err := emu.Write([]byte("tab" + itoa(i) + "\r\n")); err != nil {
			t.Fatal(err)
		}
		tab := newTabState(&fakeSession{}, emu, 9)
		tab.search = search.New(a.bufferLineCells)
		a.tabs = append(a.tabs, tab)
	}
	a.loadTab(0)
	return a
}

// pressKeys feeds rune keys through the normal key path.
func pressKeys(t *testing.T, a *App, s string) {
	t.Helper()
	for _, r := range s {
		a.handleKey(keybind.Key{Code: keybind.CodeRune, Rune: r})
	}
}

// screenText renders the live screen of the focused tab.
func screenText(a *App) string {
	var sb strings.Builder
	for y := 0; y < a.emu.Height(); y++ {
		for x := 0; x < a.emu.Width(); x++ {
			sb.WriteString(a.emu.Cell(x, y).Content)
		}
	}
	return sb.String()
}

func TestSwitchTabSwapsState(t *testing.T) {
	a := newTabTestApp(t, 2)
	if !strings.Contains(screenText(a), "tab0") || strings.Contains(screenText(a), "tab1") {
		t.Fatalf("tab0 buffer wrong: %q", screenText(a))
	}
	// Move tab0's cursor, then focus tab1 and move ITS cursor elsewhere.
	pressKeys(t, a, "j")
	curTab0 := a.cur
	a.switchTo(1)
	if !strings.Contains(screenText(a), "tab1") || strings.Contains(screenText(a), "tab0") {
		t.Fatalf("tab1 shows the wrong buffer: %q", screenText(a))
	}
	pressKeys(t, a, "G")
	curTab1 := a.cur
	if curTab1 == curTab0 {
		t.Fatalf("expected distinct cursors: tab0=%v tab1=%v", curTab0, curTab1)
	}
	// Back to tab0: both the buffer and the cursor must be restored.
	a.switchTo(0)
	if !strings.Contains(screenText(a), "tab0") {
		t.Fatal("switching back lost tab0's buffer")
	}
	if a.cur != curTab0 {
		t.Fatalf("tab0 cursor not restored: got %v, want %v", a.cur, curTab0)
	}
}

func TestActivateCycleWraps(t *testing.T) {
	a := newTabTestApp(t, 3)
	a.activateCycle(1)
	if a.active != 1 {
		t.Fatalf("after gt from 0: active=%d", a.active)
	}
	a.activateCycle(1)
	if a.active != 2 {
		t.Fatalf("after gt from 1: active=%d", a.active)
	}
	a.activateCycle(1)
	if a.active != 0 {
		t.Fatalf("gt must wrap: active=%d", a.active)
	}
	a.activateCycle(-1)
	if a.active != 2 {
		t.Fatalf("gT from 0 must wrap to last: active=%d", a.active)
	}
}

func TestActivateIndexClamps(t *testing.T) {
	a := newTabTestApp(t, 3)
	a.activateIndex(99)
	if a.active != 2 {
		t.Fatalf("99gt should clamp to last: active=%d", a.active)
	}
	a.activateIndex(2)
	if a.active != 1 {
		t.Fatalf("2gt: active=%d", a.active)
	}
}

func TestCountTabViaKeys(t *testing.T) {
	a := newTabTestApp(t, 3)
	a.handleKey(keybind.Key{Code: keybind.CodeRune, Rune: '2'})
	pressKeys(t, a, "gt")
	if a.active != 1 {
		t.Fatalf("2gt: active=%d, want 1", a.active)
	}
	// gT's T carries ModShift (uppercase implies shift in binding tokens).
	a.handleKey(keybind.NewRune('g', 0))
	a.handleKey(keybind.NewRune('T', keybind.ModShift))
	if a.active != 0 {
		t.Fatalf("gT cycles backward: active=%d, want 0", a.active)
	}
}

func TestGtDoesNotHijackFindUntil(t *testing.T) {
	a := newTabTestApp(t, 2)
	pressKeys(t, a, "t")
	if !a.find.pending() {
		t.Fatal("plain t must arm find-until")
	}
	pressKeys(t, a, "a") // consumed as the find target
	if a.find.pending() {
		t.Fatal("find still pending after target")
	}
}

func TestCloseActiveTabActivatesNeighbor(t *testing.T) {
	a := newTabTestApp(t, 3)
	a.switchTo(1)
	a.closeTab(1)
	if len(a.tabs) != 2 {
		t.Fatalf("tabs after close: %d", len(a.tabs))
	}
	if a.active != 1 {
		t.Fatalf("active after closing middle tab: %d, want 1", a.active)
	}
	if !strings.Contains(screenText(a), "tab2") {
		t.Fatalf("focused tab lost its own buffer: %q", screenText(a))
	}
}

func TestCloseLeadingTabKeepsFocus(t *testing.T) {
	a := newTabTestApp(t, 3)
	a.switchTo(2)
	a.closeTab(0)
	if a.active != 1 || len(a.tabs) != 2 {
		t.Fatalf("active=%d len=%d, want 1 and 2", a.active, len(a.tabs))
	}
	if !strings.Contains(screenText(a), "tab2") {
		t.Fatalf("focus shifted to the wrong tab: %q", screenText(a))
	}
}

func TestCloseLastTabQuits(t *testing.T) {
	a := newTabTestApp(t, 1)
	a.closeTab(0)
	select {
	case <-a.quit:
	default:
		t.Fatal("closing the last tab did not quit")
	}
}

func TestReapTabsRemovesExitedBackgroundTab(t *testing.T) {
	a := newTabTestApp(t, 2)
	bg := a.tabs[1]
	bg.doneOnce.Do(func() { close(bg.done) })
	a.reapTabs()
	if len(a.tabs) != 1 || a.active != 0 {
		t.Fatalf("reap: tabs=%d active=%d", len(a.tabs), a.active)
	}
}

func TestReapTabsPropagatesReadErrorAndQuits(t *testing.T) {
	a := newTabTestApp(t, 1)
	tt := a.tabs[0]
	tt.err.Store(errors.New("boom"))
	tt.doneOnce.Do(func() { close(tt.done) })
	a.reapTabs()
	err := a.childError()
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("childError = %v, want boom", err)
	}
}

// applyConfig reads the active state (a.emu) and writes config-derived
// values onto the active tab, so it must only run once a tab is loaded.
// This mirrors both the startup order in newApp and the hot-reload path.
func TestApplyConfigOnLoadedTab(t *testing.T) {
	a := newTabTestApp(t, 2)
	cfg := config.Default()
	cfg.General.Scrollback = 777
	if err := a.applyConfig(cfg); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	// The reloaded keymaps must include the default tab bindings.
	a.handleKey(keybind.NewRune('g', 0))
	a.handleKey(keybind.NewRune('t', 0))
	if a.active != 1 {
		t.Fatalf("gt after reload: active=%d, want 1", a.active)
	}
}

func TestNewTabFakeSpawn(t *testing.T) {
	spawned := 0
	old := spawnShell
	spawnShell = func(shell string, args []string, cols, rows int) (session, error) {
		spawned++
		return &fakeSession{}, nil
	}
	defer func() { spawnShell = old }()
	a := newTabTestApp(t, 1)
	a.newTab()
	if spawned != 1 {
		t.Fatalf("spawn called %d times", spawned)
	}
	if len(a.tabs) != 2 || a.active != 1 {
		t.Fatalf("tabs=%d active=%d", len(a.tabs), a.active)
	}
	if a.sess != a.tabs[1].sess {
		t.Fatal("focused session is not the newly spawned one")
	}
}

func TestNewTabViaLeaderNt(t *testing.T) {
	old := spawnShell
	spawnShell = func(shell string, args []string, cols, rows int) (session, error) {
		return &fakeSession{}, nil
	}
	defer func() { spawnShell = old }()
	a := newTabTestApp(t, 1)
	pressKeys(t, a, " ")
	pressKeys(t, a, "nt")
	if len(a.tabs) != 2 || a.active != 1 {
		t.Fatalf("leader+nt: tabs=%d active=%d", len(a.tabs), a.active)
	}
}

// eofSession ends immediately: its reader closes the tab's done channel on
// the first Read, so reaping removes the tab only if a reader was started.
type eofSession struct{}

func (eofSession) Write(p []byte) (int, error)    { return len(p), nil }
func (eofSession) Read(p []byte) (int, error)     { return 0, io.EOF }
func (eofSession) Resize(int, int) error          { return nil }
func (eofSession) Kill() error                    { return nil }
func (eofSession) Close() error                   { return nil }
func (eofSession) Name() string                   { return "eof" }
func (eofSession) Wait(ctx context.Context) error { return nil }

func TestNewTabStartsReaderAndWaiter(t *testing.T) {
	old := spawnShell
	spawnShell = func(shell string, args []string, cols, rows int) (session, error) {
		return eofSession{}, nil
	}
	defer func() { spawnShell = old }()
	a := newTabTestApp(t, 1)
	a.newTab()
	if len(a.tabs) != 2 {
		t.Fatalf("tabs after :new = %d, want 2", len(a.tabs))
	}
	// The new tab's session is already dead (EOF); if its reader/waiter
	// goroutines were started, reaping removes it again.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.reapTabs()
		if len(a.tabs) == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("new tab's reader/waiter never ran: exited session was not reaped")
}

func rowText(row []emulator.Cell) string {
	var sb strings.Builder
	for _, c := range row {
		sb.WriteString(c.Content)
	}
	return sb.String()
}

func TestStatusLineShowsTabs(t *testing.T) {
	row := make([]emulator.Cell, 80)
	statusLine(row, mode.ModeNormal, "", "pwsh.exe", 0, 0,
		defaultStatusFg, defaultStatusBg, []string{"1:pwsh", "2:cmd"}, 0)
	text := rowText(row)
	if !strings.Contains(text, "1:pwsh") || !strings.Contains(text, "2:cmd") {
		t.Fatalf("tab labels missing: %q", text)
	}
	// The active tab's label is reversed; the inactive one is not.
	start := strings.Index(text, "1:pwsh")
	for i := start; i < start+len("1:pwsh"); i++ {
		if !row[i].Reverse {
			t.Fatalf("active label cell %d not reversed", i)
		}
	}
	start = strings.Index(text, "2:cmd")
	for i := start; i < start+len("2:cmd"); i++ {
		if row[i].Reverse {
			t.Fatalf("inactive label cell %d reversed", i)
		}
	}
	// The right side still ends the line.
	if !strings.HasSuffix(text, " pwsh.exe  1,1 ") {
		t.Fatalf("right side displaced by tabs: %q", text)
	}
}

func TestStatusLineTabsCentered(t *testing.T) {
	row := make([]emulator.Cell, 80)
	statusLine(row, mode.ModeNormal, "", "pwsh.exe", 0, 0,
		defaultStatusFg, defaultStatusBg, []string{"1:pwsh", "2:cmd"}, 0)
	text := rowText(row)
	// left " NORMAL " ends at 8; right " pwsh.exe  1,1 " starts at 65.
	// Padded labels total 15 cells, so the block starts at 8+(57-15)/2=29
	// and its leading padding space puts "1:pwsh" at column 30.
	if got := strings.Index(text, "1:pwsh"); got != 30 {
		t.Fatalf("tab label starts at %d, want 30 (centered): %q", got, text)
	}
	if row[28].Content != " " || row[44].Content != " " {
		t.Fatal("expected padding gaps on both sides of the centered block")
	}
	if !strings.HasSuffix(text, " pwsh.exe  1,1 ") {
		t.Fatalf("right side displaced: %q", text)
	}
}

func TestStatusLineHidesTabsWhenSingle(t *testing.T) {
	row := make([]emulator.Cell, 60)
	statusLine(row, mode.ModeNormal, "", "cmd.exe", 0, 0,
		defaultStatusFg, defaultStatusBg, []string{"1:cmd"}, 0)
	if strings.Contains(rowText(row), "1:cmd") {
		t.Fatal("labels drawn for a single tab")
	}
}

func TestStatusLineTabsNarrowNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("statusLine panicked with tabs on narrow rows: %v", r)
		}
	}()
	labels := []string{"1:powershe", "2:cmd.exe", "3:wsl"}
	for cols := 1; cols <= 40; cols++ {
		row := make([]emulator.Cell, cols)
		statusLine(row, mode.ModeNormal, "", "powershell.exe", 3, 1,
			defaultStatusFg, defaultStatusBg, labels, 0)
	}
}
