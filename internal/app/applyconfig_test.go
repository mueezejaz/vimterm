package app

// Regression tests for config hot-reload atomicity and watcher/main-loop
// synchronization: a rejected reload used to leave keymaps, timeout and the
// scrollback limit half-applied, and the watcher read a.emu while the main
// loop swapped it.

import (
	"sync"
	"testing"

	"vimterm/internal/config"
	"vimterm/internal/emulator"
)

// rejectedCfg returns a config identical to the default except for a new
// scrollback limit and an invalid status color, so applyConfig fails after
// everything else would already have been applied.
func rejectedCfg(scrollback int) *config.Config {
	cfg := config.Default()
	cfg.General.Scrollback = scrollback
	cfg.Colors.StatusFg = "definitely-not-a-color"
	return cfg
}

func feedLines(a *App, n int) {
	for i := 0; i < n; i++ {
		_, _ = a.emu.Write([]byte("line\r\n"))
	}
}

// A failed reload must change nothing: not the committed cfg, not the
// emulator's scrollback limit, nothing.
func TestApplyConfigRejectedLeavesStateUntouched(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	old := a.cfg
	if err := a.applyConfig(rejectedCfg(5)); err == nil {
		t.Fatal("applyConfig accepted an invalid color")
	}
	if a.cfg != old {
		t.Fatal("rejected reload replaced the active config")
	}
	feedLines(a, 50)
	if n := a.emu.ScrollbackLen(); n <= 5 {
		t.Fatalf("scrollback capped at %d: rejected config's limit was applied", n)
	}
}

// A successful reload applies every derived setting, including scrollback.
func TestApplyConfigAppliesScrollback(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	cfg := config.Default()
	cfg.General.Scrollback = 5
	if err := a.applyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	feedLines(a, 50)
	if n := a.emu.ScrollbackLen(); n != 5 {
		t.Fatalf("scrollback len = %d, want capped at 5", n)
	}
}

// The watcher goroutine applies configs while the main loop swaps tabs;
// under -race this fails unless both sides synchronize on cfgMu.
func TestApplyConfigConcurrentWithTabSwitches(t *testing.T) {
	old := spawnShell
	spawnShell = func(shell string, args []string, cols, rows int) (session, error) {
		return &fakeSession{}, nil
	}
	defer func() { spawnShell = old }()
	a := realApp(t, 40, 6, "x\r\n")
	a.tabs[0].sess = &fakeSession{}
	a.setSessionMaterial(a.tabs[0].sess, a.emu)
	a.tabs = append(a.tabs, a.newFakeTab(t), a.newFakeTab(t))

	var wg sync.WaitGroup
	wg.Add(2)
	stop := make(chan struct{})
	go func() {
		defer wg.Done()
		cfg := config.Default()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = a.applyConfig(cfg)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			a.switchTo((a.active + 1) % len(a.tabs))
			a.storeCurrent()
			a.loadTab(a.active)
		}
		close(stop)
	}()
	wg.Wait()
}

// newFakeTab appends a tab whose session/emulator are inert fakes.
func (a *App) newFakeTab(t *testing.T) *tabState {
	t.Helper()
	emu := emulator.New(a.screenCols, terminalRows(a.screenRows))
	return newTabState(&fakeSession{}, emu, terminalRows(a.screenRows))
}
