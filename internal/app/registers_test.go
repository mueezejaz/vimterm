package app

// Tests for the register file: named/numbered/unnamed registers, the "
// prefix, and the clipboard bridge.

import (
	"strings"
	"testing"

	"vimterm/internal/keybind"
)

func TestDeleteDoesNotTouchClipboard(t *testing.T) {
	// The whole point of registers: dw must not clobber a previous yank.
	a, _, clip := deleteApp(t, "yank me\r\nthen delete word here\r\n")
	pressAll(t, a, "yy")
	if got := clip.String(); got != "yank me" {
		t.Fatalf("yy: clipboard = %q", got)
	}
	pressAll(t, a, "j")
	pressAll(t, a, "dw")
	if got := clip.String(); got != "yank me" {
		t.Fatalf("dw overwrote the clipboard: %q", got)
	}
	if got := regText(a, '1'); got != "then " {
		t.Fatalf("dw: \"1 register = %q", got)
	}
	if got := regText(a, 0); got != "then " {
		t.Fatalf("dw: unnamed register = %q", got)
	}
}

func TestYankGoesToZeroAndClipboard(t *testing.T) {
	a, _, clip := deleteApp(t, "hello\r\nworld\r\n")
	pressAll(t, a, "yy")
	if got := regText(a, '0'); got != "hello" {
		t.Fatalf("yy: \"0 = %q", got)
	}
	if got := clip.String(); got != "hello" {
		t.Fatalf("yy: clipboard = %q", got)
	}
	pressAll(t, a, "j")
	pressAll(t, a, "yw")
	if got := regText(a, '0'); got != "world" {
		t.Fatalf("yw: \"0 = %q", got)
	}
	if got := regText(a, '1'); got != "" {
		t.Fatalf("yw must not rotate the delete registers, \"1 = %q, want empty", got)
	}
}

func TestNumberedRegisterRotation(t *testing.T) {
	a := findApp(t, "one two three four\r\n")
	pressAll(t, a, "dw")
	pressAll(t, a, "dw")
	if got := regText(a, '1'); got != "two " {
		t.Fatalf("\"1 = %q, want the latest delete", got)
	}
	if got := regText(a, '2'); got != "one " {
		t.Fatalf("\"2 = %q, want the previous delete", got)
	}
}

func TestRegisterPrefixYankAndPaste(t *testing.T) {
	a, fs, _ := deleteApp(t, "hello world\r\n")
	pressAll(t, a, `"ayiw`)
	if got := regText(a, 'a'); got != "hello" {
		t.Fatalf("\"ayiw: register a = %q", got)
	}
	// "ap pastes register a into the shell.
	a.cur.Col = 0
	pressAll(t, a, `"ap`)
	if got := string(fs.writes); !strings.Contains(got, "hello") {
		t.Fatalf("\"ap did not paste register a, wrote %q", got)
	}
}

func TestRegisterAppendWithUppercase(t *testing.T) {
	a := findApp(t, "foo bar\r\n")
	pressAll(t, a, `"ayiw`)
	pressAll(t, a, "w")
	pressAll(t, a, `"Ayiw`)
	if got := regText(a, 'a'); got != "foobar" {
		t.Fatalf("\"A append: register a = %q, want %q", got, "foobar")
	}
}

func TestPasteClipboardRegister(t *testing.T) {
	a, fs := pasteApp(t, "clip text")
	a.regs.setNamed('a', "reg text")
	a.regs.hasUnnamed = true
	a.regs.unnamed = "unnamed"
	// "+p reads the clipboard even with a non-empty unnamed register.
	pressAll(t, a, `"+p`)
	if got := string(fs.writes); !strings.Contains(got, "clip text") {
		t.Fatalf(`"+p wrote %q`, got)
	}
	fs.writes = nil
	pressAll(t, a, `"ap`)
	if got := string(fs.writes); !strings.Contains(got, "reg text") {
		t.Fatalf(`"ap wrote %q`, got)
	}
	fs.writes = nil
	pressAll(t, a, "p")
	if got := string(fs.writes); !strings.Contains(got, "unnamed") {
		t.Fatalf("p wrote %q, want the unnamed register", got)
	}
}

func TestPasteFallsBackToClipboard(t *testing.T) {
	// An untouched register file: p must keep pasting the clipboard, which
	// is what it has always done.
	a, fs := pasteApp(t, "clip text")
	press(t, a, keybind.NewRune('p', 0))
	if got := string(fs.writes); !strings.Contains(got, "clip text") {
		t.Fatalf("p wrote %q, want the clipboard fallback", got)
	}
}

func TestRegistersSummaryCommand(t *testing.T) {
	a := findApp(t, "hello\r\n")
	pressAll(t, a, `"byiw`)
	a.execCommand("reg")
	if msg := a.statusText(); !strings.Contains(msg, `b hello`) {
		t.Fatalf(":reg status = %q, want it to list register b", msg)
	}
}
