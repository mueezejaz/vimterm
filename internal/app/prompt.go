package app

import "vimterm/internal/keybind"

// promptKind identifies what a transient bottom-line input is for.
type promptKind int

const (
	promptSearch promptKind = iota
	promptCommand
	promptTabs
)

// prompt is a transient input shown on the status line: a search ("/query"),
// a command (":command"), or the tab switcher ("tabs>query"). It is owned by
// the main loop goroutine.
type prompt struct {
	kind   promptKind
	runes  []rune
	cursor int
	// sel is the highlighted row of the tab popup within the filtered list.
	sel int
}

func newPrompt(kind promptKind) *prompt {
	return &prompt{kind: kind}
}

func (p *prompt) insert(r rune) {
	p.runes = append(p.runes, 0)
	copy(p.runes[p.cursor+1:], p.runes[p.cursor:])
	p.runes[p.cursor] = r
	p.cursor++
}

// backspace deletes the rune before the cursor; it reports whether anything
// was deleted.
func (p *prompt) backspace() bool {
	if p.cursor == 0 {
		return false
	}
	copy(p.runes[p.cursor-1:], p.runes[p.cursor:])
	p.runes = p.runes[:len(p.runes)-1]
	p.cursor--
	return true
}

func (p *prompt) moveLeft() {
	if p.cursor > 0 {
		p.cursor--
	}
}

func (p *prompt) moveRight() {
	if p.cursor < len(p.runes) {
		p.cursor++
	}
}

// prefix returns the prompt's display prefix.
func (p *prompt) prefix() string {
	switch p.kind {
	case promptSearch:
		return "/"
	case promptTabs:
		return "tabs>"
	default:
		return ":"
	}
}

// text returns the current input without the prompt prefix.
func (p *prompt) text() string {
	return string(p.runes)
}

// display returns the prompt prefix plus the input.
func (p *prompt) display() string {
	return p.prefix() + p.text()
}

// cursorCol returns the status-line column of the input cursor, after the
// " MODE " prefix and the prompt prefix.
func (p *prompt) cursorCol(prefixLen int) int {
	return prefixLen + len([]rune(p.prefix())) + p.cursor
}

// handlePromptKey processes one key while a prompt is active. It returns the
// runes of the prompt text after the edit, or (nil, true) when the prompt has
// been committed or cancelled.
func (a *App) handlePromptKey(k keybind.Key) {
	if a.prompt.kind == promptTabs {
		a.handleTabPopupKey(k)
		return
	}
	switch k.Code {
	case keybind.CodeRune:
		if k.Mods&^(keybind.ModShift) != 0 {
			return
		}
		a.prompt.insert(k.Rune)
	case keybind.CodeBackspace:
		a.prompt.backspace()
	case keybind.CodeLeft:
		a.prompt.moveLeft()
	case keybind.CodeRight:
		a.prompt.moveRight()
	case keybind.CodeEnter:
		a.commitPrompt()
		return
	case keybind.CodeEsc:
		a.cancelPrompt()
		return
	default:
		return
	}
	if a.prompt != nil && a.prompt.kind == promptSearch {
		// Live (incremental) search: recompute matches and jump.
		a.search.SetQueryGen([]rune(a.prompt.text()), a.searchGeneration())
		a.jumpToFirstMatch()
	}
	a.dirty.Store(true)
}

// commitPrompt executes the entered search or command and closes the prompt.
func (a *App) commitPrompt() {
	p := a.prompt
	a.prompt = nil
	if p.kind == promptSearch {
		// The query was already applied live; on commit it just sticks so
		// n/N keep working. Keep the highlight and matches.
		q := p.text()
		if q == "" {
			a.search.Clear()
			a.setStatusMsg("")
			return
		}
		if n := len(a.search.Matches()); n > 0 {
			a.setStatusMsg("search: " + q + "  (" + itoa(n) + " matches)")
		} else {
			a.setStatusMsg("search: no match for " + q)
		}
		return
	}
	a.execCommand(p.text())
}

// cancelPrompt abandons the input. A tab popup closes without side effects;
// a search clears its highlight and restores the viewport position saved
// when the search started.
func (a *App) cancelPrompt() {
	kind := a.prompt.kind
	a.prompt = nil
	if kind == promptTabs {
		a.dirty.Store(true)
		return
	}
	if a.search != nil {
		a.search.Clear()
	}
	a.vp.SetOffset(a.preSearchOffset)
	a.dirty.Store(true)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
