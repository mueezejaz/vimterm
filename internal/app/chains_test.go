package app

import (
	"testing"

	"vimterm/internal/keybind"
)

// The default leader+nt binding is the shipped chain ["new_tab",
// "rename_prompt"]: it opens a shell tab and immediately asks for its name
// through the colon prompt.

func TestNewTabChainAsksForName(t *testing.T) {
	old := spawnShell
	spawnShell = func(shell string, args []string, cols, rows int) (session, error) {
		return &fakeSession{}, nil
	}
	defer func() { spawnShell = old }()
	a := newTabTestApp(t, 1)
	pressKeys(t, a, " ")
	pressKeys(t, a, "nt")
	if len(a.tabs) != 2 || a.active != 1 {
		t.Fatalf("leader+nt: tabs=%d active=%d", len(a.tabs), a.active)
	}
	if a.prompt == nil || a.prompt.kind != promptCommand || a.prompt.display() != ":rename " {
		t.Fatalf("rename prompt not prefilled: %v", a.prompt)
	}
	// The typed name goes to the prompt; enter commits the rename and ends
	// the chain there (nothing follows rename_prompt).
	pressKeys(t, a, "build")
	if got := a.prompt.display(); got != ":rename build" {
		t.Fatalf("display = %q, want :rename build", got)
	}
	a.handleKey(keybind.NewCode(keybind.CodeEnter, 0))
	if a.prompt != nil {
		t.Fatal("enter did not close the rename prompt")
	}
	if a.tabs[1].name != "build" {
		t.Fatalf("new tab name = %q, want build", a.tabs[1].name)
	}
}

func TestNewTabChainEscLeavesUnnamed(t *testing.T) {
	old := spawnShell
	spawnShell = func(shell string, args []string, cols, rows int) (session, error) {
		return &fakeSession{}, nil
	}
	defer func() { spawnShell = old }()
	a := newTabTestApp(t, 1)
	pressKeys(t, a, " ")
	pressKeys(t, a, "nt")
	a.handleKey(keybind.NewCode(keybind.CodeEsc, 0))
	if a.prompt != nil {
		t.Fatal("esc did not close the rename prompt")
	}
	if a.tabs[1].name != "" {
		t.Fatalf("esc left a name behind: %q", a.tabs[1].name)
	}
}

// TestChainStopsAtPrompt pins the chaining rule: once a step opens a prompt,
// later steps must not run, because the prompt owns all subsequent input.
func TestChainStopsAtPrompt(t *testing.T) {
	a := newTabTestApp(t, 1)
	leader, err := keybind.ParseLeader(" ")
	if err != nil {
		t.Fatal(err)
	}
	kms, err := keybind.BuildKeymaps(map[string]map[string][]string{
		"normal": {"z": {"command_prompt", "quit"}},
	}, leader)
	if err != nil {
		t.Fatal(err)
	}
	a.engine.SetKeymaps(kms)
	pressKeys(t, a, "z")
	if a.prompt == nil || a.prompt.kind != promptCommand {
		t.Fatal("command prompt did not open")
	}
	select {
	case <-a.quit:
		t.Fatal("chain ran quit although the prompt took over")
	default:
	}
}

func TestOpenRenamePromptNamesActiveTab(t *testing.T) {
	a := newTabTestApp(t, 2)
	a.switchTo(1)
	a.openRenamePrompt()
	if a.prompt == nil || a.prompt.display() != ":rename " {
		t.Fatalf("prompt = %v, want prefilled :rename ", a.prompt)
	}
	pressKeys(t, a, "second")
	a.handleKey(keybind.NewCode(keybind.CodeEnter, 0))
	if a.tabs[1].name != "second" || a.tabs[0].name != "" {
		t.Fatalf("names = %q, %q; want only active tab renamed", a.tabs[0].name, a.tabs[1].name)
	}
}
