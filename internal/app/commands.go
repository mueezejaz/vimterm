package app

import "strings"

// builtinCommands lists the colon-command names the app understands
// natively; custom commands from [commands] cannot shadow them.
var builtinCommands = map[string]bool{
	"quit":     true,
	"clear":    true,
	"shell":    true,
	"new":      true,
	"tabnew":   true,
	"close":    true,
	"tabclose": true,
}

// parseCommand splits a command line into name and arguments. Empty or
// whitespace-only lines return (commandLineNone).
func parseCommand(s string) (string, []string) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}

// execCommand dispatches a committed colon-command. Built-in commands take
// precedence over custom commands from [commands]; unknown names are
// reported.
func (a *App) execCommand(line string) {
	name, _ := parseCommand(line)
	if name == "" {
		a.setStatusMsg("")
		return
	}
	switch name {
	case "quit":
		a.requestQuit()
		return
	case "clear":
		a.emu.ClearScrollback()
		a.search.Clear()
		a.vp.GotoBottom()
		// Buffer coordinates shifted; re-derive the cursor eagerly like
		// resize does.
		a.curValid = false
		a.syncCursor()
		// The visual anchor now points at a stale absolute line far beyond
		// the new buffer, which would select a massive wrong range.
		if a.sel.Active {
			a.cancelVisual()
		}
		a.setStatusMsg("scrollback cleared")
		return
	case "shell":
		a.restartShell()
		return
	case "new", "tabnew":
		a.newTab()
		return
	case "close", "tabclose":
		a.closeTab(a.active)
		return
	}
	if seq, ok := a.customCommand(name); ok {
		a.replayKeys(seq)
		return
	}
	a.setStatusMsg("unknown command: " + name)
}
