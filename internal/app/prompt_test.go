package app

import (
	"testing"
)

func TestPromptInsertBackspace(t *testing.T) {
	p := newPrompt(promptSearch)
	for _, r := range "hel" {
		p.insert(r)
	}
	p.insert('l')
	p.insert('o')
	if got := p.text(); got != "hello" {
		t.Fatalf("text = %q, want hello", got)
	}
	if !p.backspace() {
		t.Fatal("backspace should report success")
	}
	if got := p.text(); got != "hell" {
		t.Fatalf("after backspace text = %q, want hell", got)
	}
	if p.cursor != 4 {
		t.Fatalf("cursor = %d, want 4", p.cursor)
	}
}

func TestPromptCursorEditing(t *testing.T) {
	p := newPrompt(promptCommand)
	for _, r := range "abc" {
		p.insert(r)
	}
	p.moveLeft()
	p.moveLeft()
	p.insert('X')
	if got := p.text(); got != "aXbc" {
		t.Fatalf("insert before cursor failed: %q", got)
	}
	p.moveRight()
	if !p.backspace() {
		t.Fatal("backspace must delete the char before the cursor")
	}
	if got := p.text(); got != "aXc" {
		t.Fatalf("after backspace text = %q, want aXc", got)
	}
	p.moveLeft()
	if !p.backspace() {
		t.Fatal("backspace must delete a")
	}
	if got := p.text(); got != "Xc" {
		t.Fatalf("after edits text = %q, want Xc", got)
	}
	if p.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", p.cursor)
	}
}

func TestPromptDisplay(t *testing.T) {
	s := newPrompt(promptSearch)
	s.insert('f')
	s.insert('o')
	if got := s.display(); got != "/fo" {
		t.Fatalf("search display = %q, want /fo", got)
	}
	c := newPrompt(promptCommand)
	c.insert('q')
	if got := c.display(); got != ":q" {
		t.Fatalf("command display = %q, want :q", got)
	}
}

func TestPromptCursorCol(t *testing.T) {
	p := newPrompt(promptSearch)
	for _, r := range "xy" {
		p.insert(r)
	}
	p.moveLeft()
	if got := p.cursorCol(8); got != 8+1+1 {
		t.Fatalf("cursorCol = %d, want %d", got, 8+1+1)
	}
}

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in   string
		name string
		args []string
	}{
		{"", "", nil},
		{"   ", "", nil},
		{"quit", "quit", nil},
		{"clear", "clear", nil},
		{"shell", "shell", nil},
		{"quit now", "quit", []string{"now"}},
	}
	for _, c := range cases {
		name, args := parseCommand(c.in)
		if name != c.name || len(args) != len(c.args) {
			t.Errorf("parseCommand(%q) = %q %v, want %q %v", c.in, name, args, c.name, c.args)
			continue
		}
		for i := range args {
			if args[i] != c.args[i] {
				t.Errorf("parseCommand(%q) args = %v, want %v", c.in, args, c.args)
			}
		}
	}
}

func TestKnownCommands(t *testing.T) {
	// Built-in commands must not collide with each other.
	for _, name := range []string{"quit", "clear", "shell"} {
		if !builtinCommands[name] {
			t.Errorf("%q must be a known command", name)
		}
	}
	if builtinCommands["frobnicate"] {
		t.Error("frobnicate must be unknown")
	}
}