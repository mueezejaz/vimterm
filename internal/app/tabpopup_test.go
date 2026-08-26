package app

import (
	"testing"

	"vimterm/internal/keybind"
	"vimterm/internal/render"
)

func openPopup(t *testing.T, a *App) {
	t.Helper()
	a.openTabPopup()
	if a.prompt == nil || a.prompt.kind != promptTabs {
		t.Fatal("tab popup did not open")
	}
}

func TestTabPopupOpensViaLeaderTT(t *testing.T) {
	a := newTabTestApp(t, 2)
	pressKeys(t, a, " ")
	pressKeys(t, a, "tt")
	if a.prompt == nil || a.prompt.kind != promptTabs {
		t.Fatal("leader+tt did not open the tab popup")
	}
	if got := a.prompt.display(); got != "tabs>" {
		t.Fatalf("display = %q, want tabs>", got)
	}
}

func TestTabPopupFiltersTabs(t *testing.T) {
	a := newTabTestApp(t, 3)
	openPopup(t, a)
	pressKeys(t, a, "2")
	if idx := a.filteredTabs("2"); len(idx) != 1 || idx[0] != 1 {
		t.Fatalf("filter \"2\" = %v, want [1]", idx)
	}
	// Empty query lists everything.
	if idx := a.filteredTabs(""); len(idx) != 3 {
		t.Fatalf("empty filter = %v, want all 3", idx)
	}
}

func TestTabPopupEnterSwitchesToMatch(t *testing.T) {
	a := newTabTestApp(t, 3)
	openPopup(t, a)
	pressKeys(t, a, "3")
	a.handleKey(keybind.NewCode(keybind.CodeEnter, 0))
	if a.prompt != nil {
		t.Fatal("enter did not close the popup")
	}
	if a.active != 2 {
		t.Fatalf("active = %d, want 2", a.active)
	}
}

func TestTabPopupArrowSelection(t *testing.T) {
	a := newTabTestApp(t, 3)
	openPopup(t, a)
	a.handleKey(keybind.NewCode(keybind.CodeDown, 0))
	a.handleKey(keybind.NewCode(keybind.CodeDown, 0))
	if a.prompt.sel != 2 {
		t.Fatalf("sel after two downs = %d, want 2 (last of 3)", a.prompt.sel)
	}
	a.handleKey(keybind.NewCode(keybind.CodeUp, 0))
	if a.prompt.sel != 1 {
		t.Fatalf("sel after up = %d, want 1", a.prompt.sel)
	}
	a.handleKey(keybind.NewCode(keybind.CodeUp, 0))
	a.handleKey(keybind.NewCode(keybind.CodeUp, 0))
	if a.prompt.sel != 0 {
		t.Fatalf("sel must clamp at 0, got %d", a.prompt.sel)
	}
	a.handleKey(keybind.NewCode(keybind.CodeEnter, 0))
	if a.active != 0 {
		t.Fatalf("active = %d, want 0", a.active)
	}
}

func TestTabPopupCtrlWCancel(t *testing.T) {
	a := newTabTestApp(t, 2)
	openPopup(t, a)
	pressKeys(t, a, "f")
	a.handleKey(keybind.NewRune('w', keybind.ModCtrl))
	if a.prompt != nil {
		t.Fatal("ctrl+w did not close the popup")
	}
	if a.active != 0 {
		t.Fatalf("ctrl+w switched tabs: active=%d", a.active)
	}
	// A pending search must survive a popup open/cancel round trip.
	a.search.SetQuery([]rune("keep"))
	a.openTabPopup()
	a.handleKey(keybind.NewRune('w', keybind.ModCtrl))
	if len(a.search.Query()) != 4 {
		t.Fatalf("popup cancel cleared the search query: %q", string(a.search.Query()))
	}
}

func TestTabPopupEscCancel(t *testing.T) {
	a := newTabTestApp(t, 2)
	openPopup(t, a)
	a.handleKey(keybind.NewCode(keybind.CodeEsc, 0))
	if a.prompt != nil {
		t.Fatal("esc did not close the popup")
	}
}

func TestTabPopupNoMatchKeepsTab(t *testing.T) {
	a := newTabTestApp(t, 2)
	openPopup(t, a)
	pressKeys(t, a, "zzz")
	a.handleKey(keybind.NewCode(keybind.CodeEnter, 0))
	if a.prompt != nil {
		t.Fatal("enter with no match did not close the popup")
	}
	if a.active != 0 {
		t.Fatalf("enter with no match switched tabs: active=%d", a.active)
	}
}

func TestTabPopupRender(t *testing.T) {
	a := newTabTestApp(t, 3)
	openPopup(t, a)
	a.prompt.sel = 1
	frame := render.NewFrame(40, 10)
	a.drawTabPopup(frame)

	// Three popup rows above the status line (rows 6..8), first match on
	// top; row 9 stays the status line.
	text := func(y int) string {
		var sb []rune
		for x := 0; x < 40; x++ {
			sb = append(sb, []rune(frame.Cells[y][x].Content)...)
		}
		return string(sb)
	}
	for y, want := range map[int]string{6: " 1:fake ", 7: " 2:fake ", 8: " 3:fake "} {
		if got := text(y); got[:len(want)] != want {
			t.Fatalf("popup row %d = %q, want prefix %q", y, got, want)
		}
	}
	if !frame.Cells[7][1].Reverse || frame.Cells[6][1].Reverse || frame.Cells[8][1].Reverse {
		t.Fatal("selection highlight on the wrong popup row")
	}
}

func TestTabPopupRenderNarrowNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("drawTabPopup panicked on a tiny frame: %v", r)
		}
	}()
	a := newTabTestApp(t, 3)
	openPopup(t, a)
	for cols := 1; cols <= 5; cols++ {
		for rows := 1; rows <= 3; rows++ {
			a.drawTabPopup(render.NewFrame(cols, rows))
		}
	}
}

func TestTabPopupCursorColAfterPrefix(t *testing.T) {
	p := newPrompt(promptTabs)
	p.insert('x')
	if got := p.cursorCol(8); got != 8+5+1 {
		t.Fatalf("cursorCol = %d, want %d (mode prefix + \"tabs>\" + cursor)", got, 8+5+1)
	}
}

func TestRenameTabShowsInLabelsAndPopup(t *testing.T) {
	a := newTabTestApp(t, 2)
	a.execCommand("rename build")
	if a.tabs[0].name != "build" {
		t.Fatalf("tab name = %q, want build", a.tabs[0].name)
	}
	labels, _ := tabLabels(a.tabs, 0)
	if labels[0] != "1:build" || labels[1] != "2:fake" {
		t.Fatalf("labels = %v, want [1:build 2:fake]", labels)
	}
	// The popup filter matches the custom name, and only that tab.
	a.openTabPopup()
	pressKeys(t, a, "build")
	a.handleKey(keybind.NewCode(keybind.CodeEnter, 0))
	if a.active != 0 {
		t.Fatalf("filter \"build\" selected tab %d, want 0", a.active)
	}
}

func TestRenameOnlyAffectsActiveTab(t *testing.T) {
	a := newTabTestApp(t, 2)
	a.switchTo(1)
	a.execCommand("rename second")
	if a.tabs[0].name != "" {
		t.Fatalf("background tab renamed too: %q", a.tabs[0].name)
	}
	a.switchTo(0)
	labels, _ := tabLabels(a.tabs, 0)
	if labels[0] != "1:fake" || labels[1] != "2:second" {
		t.Fatalf("labels = %v, want [1:fake 2:second]", labels)
	}
}

func TestRenameWithoutArgResets(t *testing.T) {
	a := newTabTestApp(t, 1)
	a.execCommand("rename temp")
	a.execCommand("rename")
	if a.tabs[0].name != "" {
		t.Fatalf("name after bare :rename = %q, want empty", a.tabs[0].name)
	}
	labels, _ := tabLabels(a.tabs, 0)
	if labels[0] != "1:fake" {
		t.Fatalf("label after reset = %q, want 1:fake", labels[0])
	}
}

func TestRenameLongNameTruncatesLabelNotFilter(t *testing.T) {
	a := newTabTestApp(t, 1)
	a.execCommand("rename a-very-long-project-name")
	labels, _ := tabLabels(a.tabs, 0)
	if labels[0] != "1:a-very-l" {
		t.Fatalf("label = %q, want rune-safe truncation to 1:a-very-l", labels[0])
	}
	// Filtering still works on the full name.
	if idx := a.filteredTabs("project"); len(idx) != 1 {
		t.Fatalf("filter \"project\" = %v, want [0]", idx)
	}
}

func TestRenameIsBuiltin(t *testing.T) {
	if !builtinCommands["rename"] {
		t.Fatal("rename must be a built-in command so [commands] cannot shadow it")
	}
}
