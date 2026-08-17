// Package app wires all components together and runs the main event loop.
package app

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

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
	sess *pty.Session
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

	// Status line colors from config.
	statusFg emulator.Color
	statusBg emulator.Color

	// Prompt state (owned by the main loop goroutine).
	prompt          *prompt
	search          *search.Search
	preSearchOffset int

	// Virtual cursor and visual selection in buffer coordinates.
	cur      selection.Pos
	curValid bool
	sel      selection.Selection

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

	err atomic.Value
}

// Run starts the application and blocks until it exits.
func Run(ctx context.Context, cfg *config.Config, configPath string) error {
	a, err := newApp(ctx, cfg, configPath)
	if err != nil {
		return err
	}
	defer a.cleanup()
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
	}
	a.search = search.New(a.bufferLine)
	a.macro = macro.New()

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
	gen := a.gen.Load()
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				a.emu.Write(buf[:n])
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

// startWaiter detects child process exit, honoring session generations.
func (a *App) startWaiter() {
	sess := a.sess
	gen := a.gen.Load()
	go func() {
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
	tick := newFrameTicker()
	frame := render.NewFrame(0, 0)
	var haveFrame bool

	for {
		select {
		case ev := <-a.con.Events():
			switch e := ev.(type) {
			case console.KeyEvent:
				a.handleKey(e.Key)
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
	res, action := a.engine.Feed(modeName(a.mods.Current()), k)
	switch res {
	case keybind.Matched:
		a.tracker.noteAction(action, a.engine.LastSeq())
		if fn, ok := a.actions[action]; ok {
			fn()
		}
	case keybind.NoMatch:
		// Unbound keys pass through only in insert mode; in normal mode they
		// are swallowed.
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
	// first use, so a cursor is always visible in normal mode.
	if !a.mods.Is(mode.ModeInsert) {
		if !a.curValid {
			a.syncCursor()
		}
		top := a.topAbsLine()
		if a.cur.Line >= top && a.cur.Line <= top+rows-1 {
			cell := &frame.Cells[a.cur.Line-top][a.cur.Col]
			cell.Reverse = true
			cell.Bold = true
		}
	}

	if a.prompt != nil {
		// The host cursor sits in the prompt while it is open.
		modePrefix := len(" " + a.mods.Current().String() + " ")
		frame.CursorX = a.prompt.cursorCol(modePrefix)
		frame.CursorY = frame.Rows - 1
		frame.CursorVisible = true
	}

	statusLine(frame.Cells[frame.Rows-1], a.mods.Current(), a.statusText(), a.sess.Name(), cx, cy, a.statusFg, a.statusBg)
	render.Draw(a.con, frame)
}

func (a *App) resize(cols, rows int) {
	if cols < 1 || rows < 1 {
		return
	}
	termRows := terminalRows(rows)
	_ = a.sess.Resize(cols, termRows)
	a.emu.Resize(cols, termRows)
	a.vp.SetRows(termRows)
	a.screenCols, a.screenRows = cols, rows
	// Buffer coordinates may have shifted; re-derive the cursor lazily.
	a.curValid = false
	if a.sel.Active {
		a.syncCursor()
		a.sel.Move(a.cur)
	}
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

// bufferLine returns the runes of one absolute buffer line, or nil when the
// line does not exist.
func (a *App) bufferLine(absLine int) []rune {
	sbLen := a.emu.ScrollbackLen()
	rows := a.emu.Height()
	if absLine < 0 || absLine >= sbLen+rows {
		return nil
	}
	cols := a.emu.Width()
	var runes []rune
	for x := 0; x < cols; x++ {
		var c emulator.Cell
		if absLine < sbLen {
			c = a.emu.ScrollbackCell(x, absLine)
		} else {
			c = a.emu.Cell(x, absLine-sbLen)
		}
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
	if m, ok := a.search.Next(top - 1); ok {
		a.jumpToAbsLine(m)
		a.moveCursorTo(m)
		return
	}
	if m, ok := a.search.Next(-1); ok {
		a.jumpToAbsLine(m)
		a.moveCursorTo(m)
	}
}

// nextSearch jumps to the next (step 1) or previous (step -1) match of the
// last committed search.
func (a *App) nextSearch(step int) {
	if len(a.search.Query()) == 0 {
		a.setStatusMsg("no previous search")
		return
	}
	a.search.SetQuery(a.search.Query())
	top := a.topAbsLine()
	var m int
	var ok bool
	if step > 0 {
		if m, ok = a.search.Next(top); !ok {
			if m, ok = a.search.Next(-1); !ok {
				a.setStatusMsg("no match")
				return
			}
		}
	} else {
		if m, ok = a.search.Prev(top); !ok {
			if m, ok = a.search.Prev(a.emu.ScrollbackLen() + a.emu.Height()); !ok {
				a.setStatusMsg("no match")
				return
			}
		}
	}
	a.jumpToAbsLine(m)
	a.moveCursorTo(m)
}

// moveCursorTo places the virtual cursor at the start of the given absolute
// line, keeping it valid.
func (a *App) moveCursorTo(absLine int) {
	a.cur = selection.Pos{Line: absLine, Col: 0}
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