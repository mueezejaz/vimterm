// Package app wires all components together and runs the main event loop.
package app

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"vimterm/internal/clipboard"
	"vimterm/internal/config"
	"vimterm/internal/console"
	"vimterm/internal/emulator"
	"vimterm/internal/keybind"
	"vimterm/internal/macro"
	"vimterm/internal/mode"
	"vimterm/internal/render"
	"vimterm/internal/screen"
	"vimterm/internal/search"
	"vimterm/internal/selection"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// App is the top-level application state.
type App struct {
	cfg  *config.Config
	con  *console.Console
	sess session
	emu  emulator.Emulator
	mods *mode.Manager
	vp   *screen.Viewport

	engine    *keybind.Engine
	actions   map[keybind.Action]func()
	quit      chan struct{}
	quitOnce  sync.Once
	stopWatch func()

	// Macro recorder, repeat tracker, and custom commands (owned by the
	// main loop goroutine).
	macro     *macro.Recorder
	tracker   repeatTracker
	repeating bool
	cmdSeqs   map[string][]keybind.Key

	// f/F/t/T pending state (the target character is dynamic).
	find findState

	// Numeric count prefix: digits accumulate in count, then hand off to
	// the next count-aware action via cnt.
	count int
	cnt   int

	// clipRead returns the paste text; overridable in tests.
	clipRead func() (string, error)
	// clipWrite stores text on the clipboard; overridable in tests.
	clipWrite func(string) error

	// mouseAnchor is where a mouse drag began (from the preceding click).
	mouseAnchor selection.Pos

	// Status line colors from config.
	statusFg emulator.Color
	statusBg emulator.Color

	// Host console default colors (from the console color table), used to
	// draw the virtual cursor as a solid block that stays visible on search
	// highlights and selections. haveTheme is false when unavailable.
	themeFg   emulator.Color
	themeBg   emulator.Color
	haveTheme bool

	// Prompt state (owned by the main loop goroutine).
	prompt          *prompt
	search          *search.Search
	preSearchOffset int

	// Virtual cursor and visual selection in buffer coordinates.
	cur      selection.Pos
	curValid bool
	sel      selection.Selection

	// insertCursorOverride, when valid, overrides the host cursor position
	// for one render frame so the cursor appears at the intended position
	// immediately after entering insert mode, rather than briefly flashing
	// at the stale shell cursor position.
	insertCursorOverride   selection.Pos
	insertCursorOverrideOk bool

	// altScreen reports whether the active tab's child is in the alternate
	// screen (full-screen app such as nvim). While it is, the child gets
	// the full terminal height and the status line is merged with the
	// app's own.
	altScreen bool

	// Virtual cursor blinking: curBlink is the visible phase; lastInput is
	// the time of the last key/mouse/resize event, resetting the cursor to
	// solid so it does not blink while the user is active.
	curBlink  bool
	lastInput time.Time

	// The open tabs. The focused tab's state is materialized into the
	// corresponding App fields above (sess, emu, vp, search, cur, sel,
	// ...); tabs[active] mirrors them while it is focused and preserves
	// them when another tab takes over. Only the main loop goroutine may
	// switch tabs or touch the slice.
	tabs   []*tabState
	active int

	// err holds the read error of whichever session exited with one; it is
	// returned from Run after the last tab closes.
	err atomic.Value

	statusMu    sync.Mutex
	statusMsg_  string
	statusSetAt time.Time

	// cfgMu guards the config-derived state that the config watcher
	// goroutine writes (applyConfig) while the main loop reads it: cfg,
	// statusFg, statusBg and cmdSeqs.
	cfgMu sync.RWMutex

	dirty         atomic.Bool
	mouseMode     atomic.Int32 // 0=off, 1=on; updated by vt callbacks
	once          sync.Once
	screenCleared bool
	screenCols    int
	screenRows    int
	r             *render.Renderer
}

// Run starts the application and blocks until it exits.
func Run(ctx context.Context, cfg *config.Config, configPath string) error {
	a, err := newApp(ctx, cfg, configPath)
	if err != nil {
		return err
	}
	// Log the debug log file path so the user can find it.
	mouseDebugLog("vimterm started, mouse debug log active")
	defer a.cleanup()
	// Restore the console before letting a panic propagate: leaving the
	// host terminal in raw mode (no echo, no line editing) after a crash
	// is worse than the crash itself. cleanup is idempotent, so calling it
	// here and in the deferred cleanup is safe.
	defer func() {
		if r := recover(); r != nil {
			a.cleanup()
			panic(r)
		}
	}()
	return a.loop(ctx)
}

func newApp(ctx context.Context, cfg *config.Config, configPath string) (*App, error) {
	con, err := console.Init()
	if err != nil {
		return nil, err
	}

	// Wire up mouse debug logging.
	con.MouseDebug = func(posx, posy int, btnState, flags, ctrlKey, prev uint32) {
		mouseDebugLog("RAW MOUSE: pos=(%d,%d) btnState=0x%x flags=0x%x ctrlKey=0x%x prev=0x%x",
			posx, posy, btnState, flags, ctrlKey, prev)
	}

	cols, rows, err := con.Size()
	if err != nil {
		con.Close()
		return nil, fmt.Errorf("app: get size: %w", err)
	}
	termRows := terminalRows(rows)

	a := &App{
		cfg:        cfg,
		con:        con,
		mods:       mode.NewManager(),
		engine:     keybind.NewEngine(),
		quit:       make(chan struct{}),
		screenCols: cols,
		screenRows: rows,
		curBlink:   true,
		lastInput:  time.Now(),
		r:          render.New(),
	}

	t, err := a.spawnTab(cfg.General.Shell, cfg.General.ShellArgs, cols, termRows)
	if err != nil {
		con.Close()
		return nil, err
	}

	if fg, bg, ok := con.ThemeColors(); ok {
		a.themeFg, a.themeBg, a.haveTheme = fg, bg, true
	}
	a.macro = macro.New()
	a.clipRead = clipboard.GetText
	a.clipWrite = clipboard.SetText

	// Materialize the first tab before applyConfig: it reads the active
	// state (a.emu) and stores config-derived values on the tab.
	a.tabs = []*tabState{t}
	a.loadTab(0)
	if err := a.applyConfig(cfg); err != nil {
		con.Close()
		t.sess.Kill()
		t.sess.Close()
		return nil, err
	}
	a.actions = a.actionMap()
	a.startReader(t)
	a.startWaiter(t)

	// Hot-reload the config.
	if configPath != "" {
		a.stopWatch = config.Watch(configPath, time.Second, func(cfg *config.Config, err error) {
			if err != nil {
				a.setStatusMsg("config error: " + err.Error())
				return
			}
			if err := a.applyConfig(cfg); err != nil {
				a.setStatusMsg("config error: " + err.Error())
				return
			}
			a.setStatusMsg("config reloaded")
		})
	}

	// Show the debug log file path in the status bar.
	if mouseLog != nil {
		a.setStatusMsg("mouse debug: " + mouseLog.Name())
	}

	return a, nil
}

// session is the subset of a shell session the app uses; *pty.Session
// satisfies it, and tests can substitute a fake.
type session interface {
	Write(p []byte) (int, error)
	Read(p []byte) (int, error)
	Resize(cols, rows int) error
	Kill() error
	Close() error
	Name() string
	Wait(ctx context.Context) error
}

// spawnTab starts one shell session and builds its tab state around it.
func (a *App) spawnTab(shell string, args []string, cols, rows int) (*tabState, error) {
	sess, err := spawnShell(shell, args, cols, rows)
	if err != nil {
		return nil, err
	}
	emu := emulator.New(cols, rows)
	emu.SetScrollbackSize(a.scrollbackSize())
	emu.SetCallbacks(vt.Callbacks{
		EnableMode: func(mode ansi.Mode) {
			// Sync App-level flag from the emulator's authoritative count.
			a.mouseMode.Store(boolToInt(emu.IsMouseTracking()))
			mouseDebugLog("VT ENABLE: mode=%v mouseTracking=%v", mode, emu.IsMouseTracking())
		},
		DisableMode: func(mode ansi.Mode) {
			a.mouseMode.Store(boolToInt(emu.IsMouseTracking()))
			mouseDebugLog("VT DISABLE: mode=%v mouseTracking=%v", mode, emu.IsMouseTracking())
		},
	})
	t := newTabState(sess, emu, rows)
	t.search = search.New(a.bufferLineCells)
	t.cols, t.rows = cols, rows
	t.sb = a.scrollbackSize()
	return t, nil
}

// startReader pipes child output into the emulator. It captures the session
// and generation at start; on EOF it only closes the tab's done channel if
// it still owns the current generation (i.e. the session has not been
// restarted or the tab closed).
func (a *App) startReader(t *tabState) {
	sess := t.sess
	emu := t.emu
	gen := t.gen.Load()
	go func() {
		defer a.restoreOnPanic()
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				emu.Write(buf[:n])
				t.outBytes.Add(int64(n))
				a.dirty.Store(true)
			}
			if err != nil {
				// Only a reader of the current generation may record the
				// error or close done: a superseded session's teardown
				// error (Kill/Close always break its pipe) must neither
				// poison t.err nor signal this tab's exit.
				if gen == t.gen.Load() {
					if err != io.EOF {
						t.err.Store(err)
					}
					t.doneOnce.Do(func() { close(t.done) })
				}
				return
			}
		}
	}()
}

// restoreOnPanic cleans up the console (idempotent) before re-panicking, so
// a crash in a background goroutine never leaves the host terminal in raw
// mode.
func (a *App) restoreOnPanic() {
	if r := recover(); r != nil {
		a.cleanup()
		panic(r)
	}
}

// startWaiter detects child process exit, honoring session generations.
func (a *App) startWaiter(t *tabState) {
	sess := t.sess
	gen := t.gen.Load()
	go func() {
		defer a.restoreOnPanic()
		_ = sess.Wait(context.Background())
		if gen == t.gen.Load() {
			t.doneOnce.Do(func() { close(t.done) })
		}
	}()
}

// applyConfig rebuilds the keymaps and runtime settings from a config. It
// returns an error (leaving the app in its previous state) if the config is
// invalid: every value is parsed and validated before any state is touched,
// so a rejected reload cannot leave a half-applied hybrid behind. It may
// run on the config-watcher goroutine, so the fields it writes (cfg,
// statusFg, statusBg, cmdSeqs) are guarded by cfgMu and read only through
// the accessors below; the a.emu reference it reads goes through
// activeEmulator for the same reason (the main loop swaps it without any
// other synchronization).
func (a *App) applyConfig(cfg *config.Config) error {
	leader, err := keybind.ParseLeader(cfg.General.Leader)
	if err != nil {
		return fmt.Errorf("config: leader: %w", err)
	}
	tables := cfg.Keybindings.ActionTables()
	bindings := map[string]map[string][]string{
		modeName(mode.ModeNormal):     tables["normal"],
		modeName(mode.ModeInsert):     tables["insert"],
		modeName(mode.ModeVisual):     tables["visual"],
		modeName(mode.ModeVisualLine): tables["visual"],
	}
	keymaps, err := keybind.BuildKeymaps(bindings, leader)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	timeout := time.Duration(cfg.General.Timeoutlen) * time.Millisecond

	// Custom commands: name -> key sequence.
	seqs := make(map[string][]keybind.Key, len(cfg.Commands))
	for name, token := range cfg.Commands {
		if name == "" {
			return fmt.Errorf("config: commands: empty name")
		}
		seq, err := keybind.ParseSequence(token, leader)
		if err != nil {
			return fmt.Errorf("config: command %q: %w", name, err)
		}
		seqs[name] = seq
	}

	// Status line colors.
	statusFg, statusBg := defaultStatusFg, defaultStatusBg
	if c, ok := config.ParseHexColor(cfg.Colors.StatusFg); ok {
		statusFg = emulator.Color{R: c.R, G: c.G, B: c.B}
	} else if cfg.Colors.StatusFg != "" {
		return fmt.Errorf("config: colors: status_fg: invalid color %q", cfg.Colors.StatusFg)
	}
	if c, ok := config.ParseHexColor(cfg.Colors.StatusBg); ok {
		statusBg = emulator.Color{R: c.R, G: c.G, B: c.B}
	} else if cfg.Colors.StatusBg != "" {
		return fmt.Errorf("config: colors: status_bg: invalid color %q", cfg.Colors.StatusBg)
	}

	// Everything validated: commit the new state.
	a.engine.SetKeymaps(keymaps)
	a.engine.SetTimeout(timeout)
	activeEmulator(a).SetScrollbackSize(cfg.General.Scrollback)

	a.cfgMu.Lock()
	a.cmdSeqs = seqs
	a.statusFg, a.statusBg = statusFg, statusBg
	a.cfg = cfg
	a.cfgMu.Unlock()
	a.dirty.Store(true)
	return nil
}

// statusStyle returns the configured status line colors.
func (a *App) statusStyle() (fg, bg emulator.Color) {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.statusFg, a.statusBg
}

// statusMergeMode returns the configured status_merge mode.
func (a *App) statusMergeMode() string {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.General.StatusMerge
}

// shellCommand returns the configured shell program and its arguments.
func (a *App) shellCommand() (string, []string) {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.General.Shell, a.cfg.General.ShellArgs
}

// scrollbackSize returns the configured scrollback line limit.
func (a *App) scrollbackSize() int {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.General.Scrollback
}

// customCommand returns the key sequence bound to a custom colon-command.
func (a *App) customCommand(name string) ([]keybind.Key, bool) {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	seq, ok := a.cmdSeqs[name]
	return seq, ok
}

// activeEmulator returns the active tab's emulator reference. The main loop
// swaps a.emu without any other synchronization, so the config watcher must
// read it through this helper.
func activeEmulator(a *App) emulator.Emulator {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.emu
}

// setSessionMaterial swaps the materialized session/emulator pair of the
// active state under cfgMu, keeping the watcher's reads consistent.
func (a *App) setSessionMaterial(sess session, emu emulator.Emulator) {
	a.cfgMu.Lock()
	a.sess, a.emu = sess, emu
	a.cfgMu.Unlock()
}

func (a *App) loop(ctx context.Context) error {
	ticker := newFrameTicker()
	defer ticker.Stop()
	tick := ticker.C
	frame := render.NewFrame(0, 0)
	var haveFrame bool

	for {
		select {
		case ev := <-a.con.Events():
			a.lastInput = time.Now()
			a.curBlink = true
			switch e := ev.(type) {
			case console.KeyEvent:
				a.handleKey(e.Key)
			case console.MouseEvent:
				a.handleMouse(e)
			case console.ResizeEvent:
				a.resize(e.Cols, e.Rows)
			}
			a.dirty.Store(true)

		case <-a.quit:
			if err := a.childError(); err != nil {
				return fmt.Errorf("app: child read: %w", err)
			}
			return nil

		case <-ctx.Done():
			return nil

		case <-tick:
			// Reap tabs whose session ended. Closing the last tab quits
			// the app; a read error is surfaced through the quit path.
			a.reapTabs()
			// Cursor blink: once idle, alternate the virtual cursor's
			// visibility on the blink cadence. Any input resets it to solid.
			if want := cursorBlinkPhase(time.Now(), a.lastInput); want != a.curBlink {
				a.curBlink = want
				a.dirty.Store(true)
			}
			if !a.dirty.Swap(false) {
				continue
			}
			if a.screenCols == 0 || a.screenRows == 0 {
				continue
			}
			if !haveFrame || frame.Cols != a.screenCols || frame.Rows != a.screenRows {
				if !haveFrame || !frame.ResetFrame(a.screenCols, a.screenRows) {
					frame = render.NewFrame(a.screenCols, a.screenRows)
				}
				haveFrame = true
			}
			a.renderFrame(frame)
		}
	}
}

// handleKey routes one key press: prompts first, then macro bookkeeping,
// then the keybinding engine.
func (a *App) handleKey(k keybind.Key) {
	if a.prompt != nil {
		a.handlePromptKey(k)
		return
	}
	if a.macro.IsPending() || a.macro.IsRecording() {
		switch a.macro.Feed(k) {
		case macro.OutcomePending:
			a.setStatusMsg("awaiting register")
			a.dirty.Store(true)
			return
		case macro.OutcomeStarted:
			a.setStatusMsg("recording @" + string(a.macro.CurrentReg()))
			a.dirty.Store(true)
			return
		case macro.OutcomeReplayed:
			a.playMacroSeq(a.macro.ReplayedSeq())
			return
		case macro.OutcomeNoRegister:
			a.setStatusMsg("empty register")
			a.dirty.Store(true)
			return
		case macro.OutcomeIgnored, macro.OutcomeRecorded:
			// Not macro-related (or recorded): fall through to the engine.
		}
	}
	if a.find.pending() {
		// f/F/t/T: consume the next printable key as the target. Any other
		// key cancels the pending find and is handled normally.
		if ch, ok := isTarget(k); ok {
			a.doFind(a.find.pd, a.find.pu, ch)
			a.dirty.Store(true)
			return
		}
		a.find.clear()
		a.cnt = 0
	}
	if !a.mods.Is(mode.ModeInsert) {
		// Count prefix: digits accumulate in normal and visual modes. A
		// leading zero is not a count and falls through to the engine.
		if isDigit(k) {
			d := int(k.Rune - '0')
			if a.count != 0 || d != 0 {
				a.count = a.count*10 + d
				if a.count > maxCount {
					a.count = maxCount
				}
				a.dirty.Store(true)
				return
			}
		}
	}
	res, actions := a.engine.Feed(modeName(a.mods.Current()), k)
	switch res {
	case keybind.Matched:
		a.tracker.noteAction(actions[0], a.engine.LastSeq())
		if countAware[actions[0]] {
			a.cnt = a.count
		}
		a.count = 0
		for _, act := range actions {
			fn, ok := a.actions[act]
			if !ok {
				continue
			}
			fn()
			// A prompt swallows every following key, so later chain steps
			// must not run while it waits for input.
			if a.prompt != nil {
				break
			}
		}
	case keybind.NoMatch:
		// Unbound keys pass through only in insert mode; in normal mode they
		// are swallowed.
		a.count = 0
		if a.mods.Is(mode.ModeInsert) {
			a.tracker.noteBurst(k)
			a.passthrough(k)
		}
	case keybind.Waiting:
		// Partial sequence; waiting for more keys.
	}
}

// playMacroSeq replays a recorded register's keys through the normal key
// path. Nested recording or playback is ignored.
func (a *App) playMacroSeq(seq []keybind.Key) {
	if a.repeating {
		return
	}
	a.repeating = true
	a.macro.SetReplaying(true)
	defer func() {
		a.macro.SetReplaying(false)
		a.repeating = false
	}()
	for _, k := range seq {
		a.handleKey(k)
	}
}

func (a *App) passthrough(k keybind.Key) {
	bytes := console.KeyToBytes(k)
	if len(bytes) == 0 {
		return
	}
	if _, err := a.sess.Write(bytes); err != nil {
		a.setStatusMsg("write error: " + err.Error())
	}
}

func (a *App) renderFrame(frame *render.Frame) {
	if !a.screenCleared {
		// Clear any leftover host-terminal content once at startup.
		_, _ = a.con.Write([]byte("\x1b[2J"))
		a.screenCleared = true
	}

	// Track alternate-screen transitions: a full-screen app (nvim) takes the
	// full height so its own status line lands on the bottom row, where
	// vimterm's transient messages can overlay it.
	if alt := a.emu.IsAltScreen(); alt != a.altScreen {
		mouseDebugLog("ALT SCREEN: %v -> %v", a.altScreen, alt)
		a.altScreen = alt
		a.applyChildRows(a.screenCols, a.screenRows)
		a.curValid = false
	}

	sbLen := a.emu.ScrollbackLen()
	a.vp.SetMax(sbLen)
	offset := a.vp.Offset()
	rows, cols := a.emu.Height(), a.emu.Width()
	bufBottom := sbLen + rows - 1

	// Batch-read the live screen cells under a single emulator lock.
	screenBuf := make([]emulator.Cell, rows*cols)
	if rows > 0 && cols > 0 {
		a.emu.ReadCells(0, 0, cols, rows, screenBuf)
	}
	var sbRow []emulator.Cell
	var sbRowLine int = -1

	for y := 0; y < rows; y++ {
		absLine := bufBottom - offset - (rows - 1 - y)
		for x := 0; x < cols; x++ {
			var c emulator.Cell
			switch {
			case absLine < 0:
				c = emulator.Cell{Content: " ", Width: 1}
			case absLine < sbLen:
				// Cache scrollback row reads (one lock per visible line).
				if absLine != sbRowLine {
					sbRow = make([]emulator.Cell, cols)
					a.emu.ReadScrollbackCells(0, absLine, cols, sbRow)
					sbRowLine = absLine
				}
				c = sbRow[x]
			default:
				c = screenBuf[(absLine-sbLen)*cols+x]
			}
			frame.Cells[y][x] = c
		}
	}

	cx, cy := a.emu.Cursor()
	if a.insertCursorOverrideOk {
		cx, cy = a.insertCursorOverride.Col, a.insertCursorOverride.Line-a.emu.ScrollbackLen()
		a.insertCursorOverrideOk = false
	}
	frame.CursorX, frame.CursorY = cx, cy
	// The host cursor is shown only in insert mode; normal and visual modes
	// use the virtual cursor (drawn below). Prompts override this.
	frame.CursorVisible = a.mods.Is(mode.ModeInsert) && a.prompt == nil

	// Search highlight: mark matches on the visible lines.
	if len(a.search.Query()) > 0 {
		for y := 0; y < rows; y++ {
			absLine := bufBottom - offset - (rows - 1 - y)
			a.search.Highlight(frame.Cells[y], absLine)
		}
	}

	// Visual selection: reverse the selected cells.
	if a.sel.Active {
		for y := 0; y < rows; y++ {
			absLine := bufBottom - offset - (rows - 1 - y)
			for x := 0; x < cols; x++ {
				if a.sel.Contains(selection.Pos{Line: absLine, Col: x}) {
					frame.Cells[y][x].Reverse = true
				}
			}
		}
	}

	// Virtual cursor: marks the position in normal and visual modes. It is
	// materialized from the shell cursor (or viewport top when scrolled) on
	// first use, so a cursor is always visible in normal mode, and it blinks
	// once the user goes idle.
	if !a.mods.Is(mode.ModeInsert) {
		if !a.curValid {
			a.syncCursor()
		}
		if a.curBlink {
			top := a.topAbsLine()
			if a.cur.Line >= top && a.cur.Line <= top+rows-1 {
				cell := &frame.Cells[a.cur.Line-top][a.cur.Col]
				if a.haveTheme {
					// A solid block in the cell's inverted rendered colors: on a
					// highlighted cell the cursor lands on the opposite color
					// pair of the highlight instead of blending into it.
					cell.Fg, cell.Bg = cursorBlockStyle(*cell, a.themeFg, a.themeBg)
					cell.Reverse = false
					cell.Bold = true
				} else {
					cell.Reverse = true
					cell.Bold = true
				}
			}
		}
	}

	if a.prompt != nil {
		// The host cursor sits in the prompt while it is open.
		modePrefix := len(" " + a.mods.Current().String() + " ")
		frame.CursorX = a.prompt.cursorCol(modePrefix)
		frame.CursorY = frame.Rows - 1
		frame.CursorVisible = true
	}

	if !a.altScreen || a.prompt != nil {
		fg, bg := a.statusStyle()
		labels, active := tabLabels(a.tabs, a.active)
		statusLine(frame.Cells[frame.Rows-1], a.mods.Current(), a.statusText(), a.sess.Name(), cx, cy, fg, bg, labels, active)
	} else if msg := a.statusMsg(); msg != "" && a.vp.Offset() == 0 &&
		mergeAllowed(frame.Cells[frame.Rows-1], a.statusMergeMode()) {
		// Full-screen app at the bottom of its screen: overlay the transient
		// message on the left edge of its own status line.
		fg, bg := a.statusStyle()
		overlayStatusMessage(frame.Cells[frame.Rows-1], a.mods.Current(), msg, fg, bg)
	}
	if a.prompt != nil && a.prompt.kind == promptTabs {
		a.drawTabPopup(frame)
	}
	a.r.Draw(a.con, frame)
}

func (a *App) resize(cols, rows int) {
	if cols < 1 || rows < 1 {
		return
	}
	a.applyChildRows(cols, rows)
	a.screenCols, a.screenRows = cols, rows
	// Buffer coordinates may have shifted; re-derive the cursor lazily.
	a.curValid = false
	if a.sel.Active {
		a.syncCursor()
		a.sel.Move(a.cur)
	}
}

// applyChildRows resizes the active child session, emulator and viewport to
// the height the child gets: full height inside an alternate screen (status
// merging), one row less otherwise (the status line). It also catches the
// active tab up on scrollback changes that happened while it was in the
// background. Runs on the main loop goroutine only.
func (a *App) applyChildRows(cols, rows int) {
	termRows := a.childRows(rows)
	t := a.tabs[a.active]
	if want := a.scrollbackSize(); t.sb != want {
		a.emu.SetScrollbackSize(want)
		t.sb = want
	}
	if t.cols == cols && t.rows == termRows {
		return
	}
	mouseDebugLog("RESIZE: ConPTY %dx%d -> %dx%d (altScreen=%v, hostRows=%d, childRows=%d)",
		t.cols, t.rows, cols, termRows, a.altScreen, rows, termRows)
	_ = a.sess.Resize(cols, termRows)
	a.emu.Resize(cols, termRows)
	a.vp.SetRows(termRows)
	t.cols, t.rows = cols, termRows
}

// childRows returns the child's terminal height for a host of the given
// height.
func (a *App) childRows(hostRows int) int {
	if hostRows < 2 {
		return 1
	}
	if a.altScreen && a.statusMergeMode() != "never" {
		return hostRows
	}
	return hostRows - 1
}

// terminalRows reserves one row for the status line.
func terminalRows(hostRows int) int {
	if hostRows < 2 {
		return 1
	}
	return hostRows - 1
}

func (a *App) requestQuit() {
	a.quitOnce.Do(func() { close(a.quit) })
}

func (a *App) setStatusMsg(msg string) {
	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	a.statusMsg_ = msg
	a.statusSetAt = time.Now()
}

// statusMsg returns the current transient status message, or "" once it is
// older than a few seconds.
func (a *App) statusMsg() string {
	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	if a.statusMsg_ == "" || time.Since(a.statusSetAt) > 4*time.Second {
		return ""
	}
	return a.statusMsg_
}

func (a *App) cleanup() {
	a.once.Do(func() {
		if a.stopWatch != nil {
			a.stopWatch()
		}
		for _, t := range a.tabs {
			if t.sess != nil {
				_ = t.sess.Kill()
				_ = t.sess.Close()
			}
		}
		if a.con != nil {
			a.con.Close()
		}
	})
}

// statusText is the transient message shown on the status line: the open
// prompt's live text, or a fading status message.
func (a *App) statusText() string {
	if a.prompt != nil {
		return a.prompt.display()
	}
	return a.statusMsg()
}

// topAbsLine is the absolute buffer line number shown at the top of the
// viewport (scrollback lines 0..sbLen-1, screen lines sbLen..sbLen+rows-1).
func (a *App) topAbsLine() int {
	return a.emu.ScrollbackLen() - a.vp.Offset()
}

// searchGeneration returns the active tab's monotonic output generation for
// search cache invalidation, or -1 (never cached) when no tab materializes
// the active state.
func (a *App) searchGeneration() int {
	if a.active < 0 || a.active >= len(a.tabs) {
		return -1
	}
	return int(a.tabs[a.active].outBytes.Load())
}

// bufferLineCells returns the cells of one absolute buffer line, or nil when
// the line does not exist.
func (a *App) bufferLineCells(absLine int) []emulator.Cell {
	sbLen := a.emu.ScrollbackLen()
	rows := a.emu.Height()
	if absLine < 0 || absLine >= sbLen+rows {
		return nil
	}
	cols := a.emu.Width()
	cells := make([]emulator.Cell, 0, cols)
	for x := 0; x < cols; x++ {
		if absLine < sbLen {
			cells = append(cells, a.emu.ScrollbackCell(x, absLine))
		} else {
			cells = append(cells, a.emu.Cell(x, absLine-sbLen))
		}
	}
	return cells
}

// bufferLine returns the runes of one absolute buffer line, or nil when the
// line does not exist.
func (a *App) bufferLine(absLine int) []rune {
	runes, _ := a.bufferLineRow(absLine)
	return runes
}

// bufferLineRow returns the runes of one absolute buffer line together with
// the starting cell column of each rune, or nil when the line does not
// exist. Wide characters span two cells but contribute one rune, so the
// columns are not simply 0..n-1.
func (a *App) bufferLineRow(absLine int) ([]rune, []int) {
	cells := a.bufferLineCells(absLine)
	if cells == nil {
		return nil, nil
	}
	row := rowOf(cells)
	return row.runes, row.cols
}

// jumpToAbsLine scrolls the viewport so the given absolute line is at the
// top of the view.
func (a *App) jumpToAbsLine(absLine int) {
	sbLen := a.emu.ScrollbackLen()
	a.vp.SetMax(sbLen)
	offset := sbLen - absLine
	if offset < 0 {
		offset = 0
	}
	a.vp.SetOffset(offset)
}

// openSearch starts an incremental search from the current viewport position.
func (a *App) openSearch() {
	a.preSearchOffset = a.vp.Offset()
	a.prompt = newPrompt(promptSearch)
	a.search.SetQuery(nil)
	a.dirty.Store(true)
}

// jumpToFirstMatch moves the viewport to the first match at or below the
// current top line, wrapping to the first match if there is none. The
// virtual cursor follows the match.
func (a *App) jumpToFirstMatch() {
	if len(a.search.Matches()) == 0 {
		return
	}
	top := a.topAbsLine()
	if m, ok := a.search.Next(top, -1); ok {
		a.jumpToAbsLine(m.Line)
		a.moveCursorTo(m.Line, m.Col)
		return
	}
	if m, ok := a.search.Next(-1, -1); ok {
		a.jumpToAbsLine(m.Line)
		a.moveCursorTo(m.Line, m.Col)
	}
}

// nextSearch jumps to the next (step 1) or previous (step -1) match of the
// last committed search, from the virtual cursor position (wrapping around
// the buffer ends).
func (a *App) nextSearch(step int) {
	if len(a.search.Query()) == 0 {
		a.setStatusMsg("no previous search")
		return
	}
	if !a.curValid {
		a.syncCursor()
	}
	// Re-scan the buffer only when new output arrived since the last scan;
	// a counted search (99999n) must not rescan on every step. The
	// generation is the tab's output byte count, not the buffer line
	// count: lines stop changing once scrollback saturates.
	a.search.Refresh(a.search.Query(), a.searchGeneration())
	var m search.Match
	var ok bool
	if step > 0 {
		if m, ok = a.search.Next(a.cur.Line, a.cur.Col); !ok {
			if m, ok = a.search.Next(-1, -1); !ok {
				a.setStatusMsg("no match")
				return
			}
		}
	} else {
		if m, ok = a.search.Prev(a.cur.Line, a.cur.Col); !ok {
			if m, ok = a.search.Prev(a.emu.ScrollbackLen()+a.emu.Height(), 1<<30); !ok {
				a.setStatusMsg("no match")
				return
			}
		}
	}
	a.jumpToAbsLine(m.Line)
	a.moveCursorTo(m.Line, m.Col)
}

// moveCursorTo places the virtual cursor at the given column of the given
// absolute line, keeping it valid.
func (a *App) moveCursorTo(absLine, col int) {
	a.cur = selection.Pos{Line: absLine, Col: col}
	a.curValid = true
	a.clampCursor()
	a.dirty.Store(true)
}

// restartShell spawns a fresh shell session in a new emulator for the
// active tab, keeping the current viewport and config.
func (a *App) restartShell() {
	cols, termRows := a.screenCols, terminalRows(a.screenRows)
	shell, shellArgs := a.shellCommand()
	sess, err := spawnShell(shell, shellArgs, cols, termRows)
	if err != nil {
		a.setStatusMsg("shell: " + err.Error())
		return
	}
	t := a.tabs[a.active]
	t.gen.Add(1)
	t.err = atomic.Value{} // drop the old session's read error, if any
	old := t.sess
	emu := emulator.New(cols, termRows)
	emu.SetScrollbackSize(a.scrollbackSize())
	emu.SetCallbacks(vt.Callbacks{
		EnableMode: func(mode ansi.Mode) {
			a.mouseMode.Store(boolToInt(emu.IsMouseTracking()))
			mouseDebugLog("VT ENABLE (restart): mode=%v mouseTracking=%v", mode, emu.IsMouseTracking())
		},
		DisableMode: func(mode ansi.Mode) {
			a.mouseMode.Store(boolToInt(emu.IsMouseTracking()))
			mouseDebugLog("VT DISABLE (restart): mode=%v mouseTracking=%v", mode, emu.IsMouseTracking())
		},
	})
	a.setSessionMaterial(sess, emu)
	a.vp.GotoBottom()
	a.search.Clear()
	a.curValid = false
	a.sel.Cancel()
	a.altScreen = false
	t.sess = sess
	t.emu = emu
	t.cols, t.rows = cols, termRows
	t.sb = a.scrollbackSize()
	t.search = a.search
	a.startReader(t)
	a.startWaiter(t)
	go func() {
		defer a.restoreOnPanic()
		_ = old.Kill()
		_ = old.Close()
	}()
	a.setStatusMsg("shell restarted")
}

// boolToInt returns 1 if b is true, 0 otherwise.
func boolToInt(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// modeName maps a mode to its config keymap name.
func modeName(m mode.Mode) string {
	switch m {
	case mode.ModeInsert:
		return "insert"
	case mode.ModeVisual:
		return "visual"
	case mode.ModeVisualLine:
		return "visual_line"
	default:
		return "normal"
	}
}
