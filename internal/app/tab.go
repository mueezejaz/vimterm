package app

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"vimterm/internal/emulator"
	"vimterm/internal/pty"
	"vimterm/internal/screen"
	"vimterm/internal/search"
	"vimterm/internal/selection"
)

// spawnShell spawns a shell session; a package variable so tests can fake it.
var spawnShell = func(shell string, args []string, cols, rows int) (session, error) {
	return pty.Spawn(shell, args, cols, rows)
}

// tabState snapshots one open shell together with its per-session view
// state. The active tab's fields are materialized into the corresponding
// App fields (a.sess, a.emu, ...) while it is focused; switching tabs swaps
// them back and forth. Everything here is owned by the main loop goroutine,
// except gen/err/done, which the per-tab reader/waiter goroutines touch:
// they capture sess, emu, gen at start, so output keeps accumulating into a
// background tab's emulator while it is not focused.
type tabState struct {
	sess   session
	emu    emulator.Emulator
	vp     *screen.Viewport
	search *search.Search

	cur         selection.Pos
	curValid    bool
	sel         selection.Selection
	mouseAnchor selection.Pos

	// altScreen reports whether this tab's child is in the alternate
	// screen; cols/rows cache the last child geometry applied to it so
	// reactivation can skip redundant resizes. sb caches the scrollback
	// limit applied to its emulator (config hot-reloads apply to the
	// active tab immediately; background tabs catch up on activation).
	altScreen bool
	cols      int
	rows      int
	sb        int

	// name is a custom tab name set with :rename; empty falls back to the
	// shell program's base name.
	name string

	preSearchOffset int

	// gen guards this tab's reader/waiter goroutines across :shell
	// restarts; done is closed by the current generation when the session
	// ends; err holds the read error, if any.
	gen      atomic.Int64
	done     chan struct{}
	doneOnce sync.Once
	err      atomic.Value
}

func newTabState(sess session, emu emulator.Emulator, rows int) *tabState {
	return &tabState{
		sess: sess,
		emu:  emu,
		vp:   screen.New(rows),
		done: make(chan struct{}),
		cols: -1,
		rows: -1,
	}
}

// store copies the active view state back into tabs[a.active].
func (a *App) storeCurrent() {
	t := a.tabs[a.active]
	a.cfgMu.RLock()
	sess, emu := a.sess, a.emu
	a.cfgMu.RUnlock()
	t.sess = sess
	t.emu = emu
	t.vp = a.vp
	t.search = a.search
	t.cur = a.cur
	t.curValid = a.curValid
	t.sel = a.sel
	t.mouseAnchor = a.mouseAnchor
	t.altScreen = a.altScreen
	t.preSearchOffset = a.preSearchOffset
}

// loadTab materializes tabs[i] as the active state.
func (a *App) loadTab(i int) {
	t := a.tabs[i]
	a.active = i
	a.setSessionMaterial(t.sess, t.emu)
	a.vp = t.vp
	a.search = t.search
	a.cur = t.cur
	a.curValid = t.curValid
	a.sel = t.sel
	a.mouseAnchor = t.mouseAnchor
	a.altScreen = t.altScreen
	a.preSearchOffset = t.preSearchOffset
}

// switchTo stores the current tab and activates index i, reapplying child
// geometry and deferred config changes in case they happened while the tab
// was in background.
func (a *App) switchTo(i int) {
	a.storeCurrent()
	a.loadTab(i)
	a.applyChildRows(a.screenCols, a.screenRows)
	a.dirty.Store(true)
}

// newTab opens a fresh shell in a new tab and focuses it.
func (a *App) newTab() {
	cols, termRows := a.screenCols, terminalRows(a.screenRows)
	shell, args := a.shellCommand()
	a.storeCurrent()
	t, err := a.spawnTab(shell, args, cols, termRows)
	if err != nil {
		a.setStatusMsg("shell: " + err.Error())
		return
	}
	a.tabs = append(a.tabs, t)
	a.loadTab(len(a.tabs) - 1)
	// Pipe the new session's output and detect its exit; without these the
	// tab renders nothing and typed input is never echoed back.
	a.startReader(t)
	a.startWaiter(t)
	a.setStatusMsg("tab " + itoa(len(a.tabs)))
}

// activateCycle moves to the next (+1) or previous (-1) tab, wrapping.
func (a *App) activateCycle(dir int) {
	if len(a.tabs) < 2 {
		a.setStatusMsg("no other tabs")
		return
	}
	next := (a.active + dir + len(a.tabs)) % len(a.tabs)
	a.switchTo(next)
	a.setStatusMsg("tab " + itoa(next+1))
}

// activateIndex jumps to the 1-based tab n (clamped).
func (a *App) activateIndex(n int) {
	i := n - 1
	if i < 0 {
		i = 0
	}
	if i >= len(a.tabs) {
		i = len(a.tabs) - 1
	}
	if i == a.active {
		return
	}
	a.switchTo(i)
	a.setStatusMsg("tab " + itoa(i+1))
}

// closeTab closes tab i. When it is the last one the app quits. The
// active-tab branch must not storeCurrent: the dying tab's state is still
// materialized in the App fields and would clobber the neighbor snapshot.
func (a *App) closeTab(i int) {
	t := a.tabs[i]
	t.gen.Add(1) // silence this tab's reader/waiter goroutines
	sess := t.sess
	go func() {
		defer a.restoreOnPanic()
		_ = sess.Kill()
		_ = sess.Close()
	}()
	a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
	switch {
	case len(a.tabs) == 0:
		a.requestQuit()
	case i < a.active:
		a.active-- // indices shifted; the focused tab keeps its materialized state
		a.dirty.Store(true)
	case i == a.active:
		next := min(i, len(a.tabs)-1)
		a.loadTab(next)
		a.applyChildRows(a.screenCols, a.screenRows)
		a.dirty.Store(true)
	default:
		a.dirty.Store(true)
	}
}

// reapTabs removes tabs whose session ended, propagating any read error.
func (a *App) reapTabs() {
	for i := 0; i < len(a.tabs); i++ {
		t := a.tabs[i]
		select {
		case <-t.done:
			if e, ok := t.err.Load().(error); ok && e != nil {
				a.err.Store(e)
			}
			a.closeTab(i)
			i-- // reconsider this index after removal
		default:
		}
	}
}

// childError returns the error recorded for an exited session, if any.
func (a *App) childError() error {
	if e, ok := a.err.Load().(error); ok && e != nil {
		return e
	}
	return nil
}

// tabName returns a tab's display name: its custom :rename name, or the
// base name of its shell program.
func tabName(t *tabState) string {
	if t.name != "" {
		return t.name
	}
	return filepath.Base(sessName(t.sess))
}

// tabLabels builds short labels for the status line, one per tab: the tab's
// 1-based index plus its name, truncated to maxTabLabelLen cells
// ("1:pwsh").
func tabLabels(tabs []*tabState, active int) ([]string, int) {
	labels := make([]string, len(tabs))
	for i, t := range tabs {
		labels[i] = itoa(i+1) + ":" + truncateRunes(tabName(t), maxTabLabelLen)
	}
	return labels, active
}

const maxTabLabelLen = 8

// truncateRunes shortens s to at most n runes without splitting one.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// renameTab names the active tab; an empty name resets it to the shell's.
func (a *App) renameTab(name string) {
	t := a.tabs[a.active]
	t.name = strings.TrimSpace(name)
	if t.name == "" {
		a.setStatusMsg("tab name reset")
	} else {
		a.setStatusMsg("tab renamed to " + t.name)
	}
	a.dirty.Store(true)
}

// sessName returns the session's name, or "" for a nil session.
func sessName(s session) string {
	if s == nil {
		return ""
	}
	return s.Name()
}
