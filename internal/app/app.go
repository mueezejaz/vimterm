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
	"vimterm/internal/pty"
	"vimterm/internal/render"
	"vimterm/internal/screen"
	"vimterm/internal/search"
	"vimterm/internal/selection"
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

	// Virtual cursor blinking: curBlink is the visible phase; lastInput is
	// the time of the last key/mouse/resize event, resetting the cursor to
	// solid so it does not blink while the user is active.
	curBlink  bool
	lastInput time.Time

	// altScreen reports whether the child is in the alternate screen
	// (full-screen app such as nvim). While it is, the child gets the full
	// terminal height and the status line is merged with the app's own.
	altScreen bool

	// gen guards the session reader/waiter goroutines across restarts: only
	// the generation matching the current session may close a.done.
	gen atomic.Int64

	statusMu    sync.Mutex
	statusMsg_  string
	statusSetAt time.Time

	dirty         atomic.Bool
	done          chan struct{}
	doneOnce      sync.Once
	once          sync.Once
	screenCleared bool
	screenCols    int
	screenRows    int
	r             *render.Renderer

	err atomic.Value
}

// Run starts the application and blocks until it exits.
func Run(ctx context.Context, cfg *config.Config, configPath string) error {
	a, err := newApp(ctx, cfg, configPath)
	if err != nil {
		return err
	}
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

	cols, rows, err := con.Size()
	if err != nil {
		con.Close()
		return nil, fmt.Errorf("app: get size: %w", err)
	}
	termRows := terminalRows(rows)

	sess, err := pty.Spawn(cfg.General.Shell, cfg.General.ShellArgs, cols, termRows)
	if err != nil {
		con.Close()
		return nil, err
	}

	emu := emulator.New(cols, termRows)
	emu.SetScrollbackSize(cfg.General.Scrollback)

	a := &App{
		cfg:        cfg,
		con:        con,
		sess:       sess,
		emu:        emu,
		mods:       mode.NewManager(),
		vp:         screen.New(termRows),
		engine:     keybind.NewEngine(),
		quit:       make(chan struct{}),
		screenCols: cols,
		screenRows: rows,
		done:       make(chan struct{}),
		curBlink:   true,
		lastInput:  time.Now(),
		r:          render.New(),
	}
	if fg, bg, ok := con.ThemeColors(); ok {
		a.themeFg, a.themeBg, a.haveTheme = fg, bg, true
	}
	a.search = search.New(a.bufferLineCells)
	a.macro = macro.New()
	a.clipRead = clipboard.GetText
	a.clipWrite = clipboard.SetText

	if err := a.applyConfig(cfg); err != nil {
		con.Close()
		sess.Kill()
		sess.Close()
		return nil, err
	}
	a.actions = a.actionMap()
	a.startReader()
	a.startWaiter()

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

// closeDone signals the main loop that the session has ended or the app is
// shutting down, exactly once.
func (a *App) closeDone() {
	a.doneOnce.Do(func() { close(a.done) })
}

// startReader pipes child output into the emulator. It captures the session
// and generation at start; on EOF it only closes a.done if it still owns the
// current generation (i.e. the session has not been restarted).
func (a *App) startReader() {
	sess := a.sess
	emu := a.emu
	gen := a.gen.Load()
	go func() {
		defer a.restoreOnPanic()
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				emu.Write(buf[:n])
				a.dirty.Store(true)
			}
			if err != nil {
				if err != io.EOF {
					a.err.Store(err)
				}
				if gen == a.gen.Load() {
					a.closeDone()
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
func (a *App) startWaiter() {
	sess := a.sess
	gen := a.gen.Load()
	go func() {
		defer a.restoreOnPanic()
		_ = sess.Wait(context.Background())
		if gen == a.gen.Load() {
			a.closeDone()
		}
	}()
}

// applyConfig rebuilds the keymaps and runtime settings from a config. It
// returns an error (leaving the app in its previous state) if the config is
// invalid.
func (a *App) applyConfig(cfg *config.Config) error {
	leader, err := keybind.ParseLeader(cfg.General.Leader)
	if err != nil {
		return fmt.Errorf("config: leader: %w", err)
	}
	bindings := map[string]map[string]string{
		modeName(mode.ModeNormal):     cfg.Keybindings.Normal,
		modeName(mode.ModeInsert):     cfg.Keybindings.Insert,
		modeName(mode.ModeVisual):     cfg.Keybindings.Visual,
		modeName(mode.ModeVisualLine): cfg.Keybindings.Visual,
	}
	keymaps, err := keybind.BuildKeymaps(bindings, leader)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	a.engine.SetKeymaps(keymaps)
	a.engine.SetTimeout(time.Duration(cfg.General.Timeoutlen) * time.Millisecond)
	a.emu.SetScrollbackSize(cfg.General.Scrollback)

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
	a.cmdSeqs = seqs

	// Status line colors.
	a.statusFg, a.statusBg = defaultStatusFg, defaultStatusBg
	if c, ok := config.ParseHexColor(cfg.Colors.StatusFg); ok {
		a.statusFg = emulator.Color{R: c.R, G: c.G, B: c.B}
	} else if cfg.Colors.StatusFg != "" {
		return fmt.Errorf("config: colors: status_fg: invalid color %q", cfg.Colors.StatusFg)
	}
	if c, ok := config.ParseHexColor(cfg.Colors.StatusBg); ok {
		a.statusBg = emulator.Color{R: c.R, G: c.G, B: c.B}
	} else if cfg.Colors.StatusBg != "" {
		return fmt.Errorf("config: colors: status_bg: invalid color %q", cfg.Colors.StatusBg)
	}

	a.cfg = cfg
	a.dirty.Store(true)
	return nil
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
			return nil

		case <-a.done:
			if err, _ := a.err.Load().(error); err != nil {
				return fmt.Errorf("app: child read: %w", err)
			}
			return nil

		case <-ctx.Done():
			return nil

		case <-tick:
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
				frame = render.NewFrame(a.screenCols, a.screenRows)
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
	res, action := a.engine.Feed(modeName(a.mods.Current()), k)
	switch res {
	case keybind.Matched:
		a.tracker.noteAction(action, a.engine.LastSeq())
		if fn, ok := a.actions[action]; ok {
			if countAware[action] {
				a.cnt = a.count
				a.count = 0
			} else {
				a.count = 0
			}
			fn()
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
		a.altScreen = alt
		a.applyChildRows(a.screenCols, a.screenRows)
		a.curValid = false
	}

	sbLen := a.emu.ScrollbackLen()
	a.vp.SetMax(sbLen)
	offset := a.vp.Offset()
	rows, cols := a.emu.Height(), a.emu.Width()
	bufBottom := sbLen + rows - 1

	for y := 0; y < rows; y++ {
		absLine := bufBottom - offset - (rows - 1 - y)
		for x := 0; x < cols; x++ {
			var c emulator.Cell
			switch {
			case absLine < 0:
				c = emulator.Cell{Content: " ", Width: 1}
			case absLine < sbLen:
				c = a.emu.ScrollbackCell(x, absLine)
			default:
				c = a.emu.Cell(x, absLine-sbLen)
			}
			frame.Cells[y][x] = c
		}
	}

	cx, cy := a.emu.Cursor()
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
		statusLine(frame.Cells[frame.Rows-1], a.mods.Current(), a.statusText(), a.sess.Name(), cx, cy, a.statusFg, a.statusBg)
	} else if msg := a.statusMsg(); msg != "" && a.vp.Offset() == 0 &&
		mergeAllowed(frame.Cells[frame.Rows-1], a.cfg.General.StatusMerge) {
		// Full-screen app at the bottom of its screen: overlay the transient
		// message on the left edge of its own status line.
		overlayStatusMessage(frame.Cells[frame.Rows-1], a.mods.Current(), msg, a.statusFg, a.statusBg)
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

// applyChildRows resizes the child session, emulator and viewport to the
// height the child gets: full height inside an alternate screen (status
// merging), one row less otherwise (the status line).
func (a *App) applyChildRows(cols, rows int) {
	termRows := a.childRows(rows)
	_ = a.sess.Resize(cols, termRows)
	a.emu.Resize(cols, termRows)
	a.vp.SetRows(termRows)
}

// childRows returns the child's terminal height for a host of the given
// height.
func (a *App) childRows(hostRows int) int {
	if hostRows < 2 {
		return 1
	}
	if a.altScreen && a.cfg.General.StatusMerge != "never" {
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
		a.closeDone()
		if a.stopWatch != nil {
			a.stopWatch()
		}
		if a.sess != nil {
			_ = a.sess.Kill()
			_ = a.sess.Close()
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
	cells := a.bufferLineCells(absLine)
	if cells == nil {
		return nil
	}
	var runes []rune
	for _, c := range cells {
		runes = append(runes, []rune(c.Content)...)
	}
	return runes
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
	a.search.SetQuery(a.search.Query())
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

// restartShell spawns a fresh shell session in a new emulator, keeping the
// current viewport and config.
func (a *App) restartShell() {
	cols, termRows := a.screenCols, terminalRows(a.screenRows)
	sess, err := pty.Spawn(a.cfg.General.Shell, a.cfg.General.ShellArgs, cols, termRows)
	if err != nil {
		a.setStatusMsg("shell: " + err.Error())
		return
	}
	a.gen.Add(1)
	old := a.sess
	a.sess = sess
	a.emu = emulator.New(cols, termRows)
	a.emu.SetScrollbackSize(a.cfg.General.Scrollback)
	a.vp.GotoBottom()
	a.search.Clear()
	a.startReader()
	a.startWaiter()
	go func() {
		_ = old.Kill()
		_ = old.Close()
	}()
	a.setStatusMsg("shell restarted")
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