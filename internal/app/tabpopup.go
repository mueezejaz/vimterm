package app

import (
	"strings"

	"vimterm/internal/emulator"
	"vimterm/internal/keybind"
	"vimterm/internal/render"
)

// maxTabPopupRows caps the popup height; with more matches than this the
// list scrolls to keep the selection visible.
const maxTabPopupRows = 10

// openTabPopup opens the tab switcher: a filterable list of open tabs drawn
// above the status line. Enter switches to the selected tab, ctrl+w or esc
// closes the popup.
func (a *App) openTabPopup() {
	a.preSearchOffset = a.vp.Offset()
	a.prompt = newPrompt(promptTabs)
	a.dirty.Store(true)
}

// filteredTabs returns the indices of tabs whose status-line label contains
// the query (case-insensitive), in tab order.
func (a *App) filteredTabs(query string) []int {
	labels, _ := tabLabels(a.tabs, a.active)
	q := strings.ToLower(query)
	var out []int
	for i, l := range labels {
		if q == "" || strings.Contains(strings.ToLower(l), q) {
			out = append(out, i)
		}
	}
	return out
}

// clampPopupSel keeps the popup selection inside the filtered list.
func clampPopupSel(sel, n int) int {
	if n <= 0 {
		return 0
	}
	if sel < 0 {
		return 0
	}
	if sel >= n {
		return n - 1
	}
	return sel
}

// handleTabPopupKey processes one key while the tab switcher is open.
func (a *App) handleTabPopupKey(k keybind.Key) {
	p := a.prompt
	switch {
	case k.Code == keybind.CodeRune && k.Mods == keybind.ModCtrl && (k.Rune == 'w' || k.Rune == 'W'):
		a.cancelPrompt()
		return
	case k.Code == keybind.CodeEsc:
		a.cancelPrompt()
		return
	case k.Code == keybind.CodeEnter:
		a.commitTabPopup()
		return
	case k.Code == keybind.CodeUp || (k.Code == keybind.CodeRune && k.Mods == keybind.ModCtrl && k.Rune == 'p'):
		p.sel--
	case k.Code == keybind.CodeDown || (k.Code == keybind.CodeRune && k.Mods == keybind.ModCtrl && k.Rune == 'n'):
		p.sel++
	case k.Code == keybind.CodeRune && k.Mods&^keybind.ModShift == 0:
		p.insert(k.Rune)
		p.sel = 0
	case k.Code == keybind.CodeBackspace && k.Mods&^keybind.ModShift == 0:
		p.backspace()
		p.sel = 0
	default:
		return
	}
	n := len(a.filteredTabs(p.text()))
	p.sel = clampPopupSel(p.sel, n)
	a.dirty.Store(true)
}

// commitTabPopup switches to the highlighted tab and closes the popup.
func (a *App) commitTabPopup() {
	p := a.prompt
	a.prompt = nil
	idx := a.filteredTabs(p.text())
	if len(idx) == 0 {
		a.setStatusMsg("no matching tab")
		a.dirty.Store(true)
		return
	}
	tab := idx[clampPopupSel(p.sel, len(idx))]
	if tab != a.active {
		a.switchTo(tab)
	}
	a.setStatusMsg("tab " + itoa(tab+1))
	a.dirty.Store(true)
}

// drawTabPopup draws the tab switcher above the status line: one row per
// matching tab, the selected row reversed. Rows are blanked first so the
// popup reads as a window over the content.
func (a *App) drawTabPopup(frame *render.Frame) {
	p := a.prompt
	idx := a.filteredTabs(p.text())
	labels, _ := tabLabels(a.tabs, a.active)
	p.sel = clampPopupSel(p.sel, len(idx))

	fg, bg := a.statusStyle()
	rows := len(idx)
	if rows > maxTabPopupRows {
		rows = maxTabPopupRows
	}
	if rows > frame.Rows-1 {
		rows = frame.Rows - 1
	}
	start := 0
	if p.sel >= rows {
		start = p.sel - rows + 1
	}
	for i := 0; i < rows; i++ {
		tabI := idx[start+i]
		// The popup block sits directly above the status line; row i is
		// the i-th row from its top, so the first match renders on top.
		y := frame.Rows - 2 - (rows - 1 - i)
		row := frame.Cells[y]
		for x := range row {
			row[x] = emulator.Cell{Content: " ", Width: 1}
		}
		text := []rune(" " + labels[tabI] + " ")
		for x, r := range text {
			if x >= len(row) {
				break
			}
			row[x] = statusCell(r, fg, bg)
			row[x].Reverse = start+i == p.sel
		}
	}
}
