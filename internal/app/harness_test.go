package app

// Shared test harness: builds an App wired the same way Run does, without a
// live shell session.

import (
	"testing"

	"vimterm/internal/config"
	"vimterm/internal/emulator"
	"vimterm/internal/keybind"
	"vimterm/internal/macro"
	"vimterm/internal/mode"
	"vimterm/internal/screen"
	"vimterm/internal/search"
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
		curBlink:   true,
	}
	a.tabs = []*tabState{newTabState(nil, emu, termRows)}
	a.active = 0
	a.engine.SetKeymaps(keymaps)
	a.search = search.New(a.bufferLineCells)
	a.actions = a.actionMap()
	a.vp.SetMax(a.emu.ScrollbackLen())
	return a
}

// clipStubs replaces the clipboard hooks with inert fakes so yank/paste
// tests never touch the real clipboard.
func (a *App) clipStubs() {
	a.clipRead = func() (string, error) { return "", nil }
	a.clipWrite = func(string) error { return nil }
}
