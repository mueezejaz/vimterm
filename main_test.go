package main

// Tests for the command line. The positional-directory argument exists so
// launchers that integrate a file manager ("open this folder in a terminal")
// can drive vimterm the same way they drive every other terminal; getting the
// parsing wrong silently opens vimterm in the wrong place, which is the exact
// failure these tests guard against.

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// defineFlags registers the same flags as main so Lookup succeeds.
func defineFlags(t *testing.T) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("config", "", "")
	fs.String("shell", "", "")
	fs.String("dir", "", "")
	fs.Bool("verbose", false, "")
	old := flag.CommandLine
	flag.CommandLine = fs
	t.Cleanup(func() { flag.CommandLine = old })
}

func TestSplitArgsFlagsOnly(t *testing.T) {
	defineFlags(t)
	flags, pos := splitArgs([]string{"-config", "a.toml", "-shell", "pwsh.exe"})
	if len(pos) != 0 {
		t.Fatalf("positionals = %q, want none", pos)
	}
	if len(flags) != 4 {
		t.Fatalf("flags = %q", flags)
	}
}

// The launcher case: a directory as the only argument.
func TestSplitArgsSinglePositional(t *testing.T) {
	defineFlags(t)
	flags, pos := splitArgs([]string{`C:\dir\sub`})
	if len(flags) != 0 {
		t.Fatalf("flags = %q, want none", flags)
	}
	if len(pos) != 1 || pos[0] != `C:\dir\sub` {
		t.Fatalf("positionals = %q", pos)
	}
}

// flag.Parse stops at the first non-flag argument, so a directory given before
// the flags would otherwise swallow them all.
func TestSplitArgsPositionalBeforeFlags(t *testing.T) {
	defineFlags(t)
	flags, pos := splitArgs([]string{`C:\dir`, "-config", "a.toml"})
	if len(pos) != 1 || pos[0] != `C:\dir` {
		t.Fatalf("positionals = %q", pos)
	}
	if len(flags) != 2 || flags[0] != "-config" || flags[1] != "a.toml" {
		t.Fatalf("flags = %q", flags)
	}
}

// A flag value must not be mistaken for a positional directory, even when the
// value happens to look like a path.
func TestSplitArgsFlagValueNotPositional(t *testing.T) {
	defineFlags(t)
	flags, pos := splitArgs([]string{"-config", `C:\cfg.toml`, "-shell", "pwsh.exe"})
	if len(pos) != 0 {
		t.Fatalf("positionals = %q, want none", pos)
	}
	if len(flags) != 4 {
		t.Fatalf("flags = %q", flags)
	}
}

// A boolean flag takes no value, so the next argument must stay positional.
func TestSplitArgsBoolFlagTakesNoValue(t *testing.T) {
	defineFlags(t)
	_, pos := splitArgs([]string{"-verbose", `C:\dir`})
	if len(pos) != 1 || pos[0] != `C:\dir` {
		t.Fatalf("positionals = %q, want the directory", pos)
	}
}

// "-flag=value" is self-contained and must not eat the following argument.
func TestSplitArgsInlineValue(t *testing.T) {
	defineFlags(t)
	flags, pos := splitArgs([]string{"-config=a.toml", `C:\dir`})
	if len(pos) != 1 || pos[0] != `C:\dir` {
		t.Fatalf("positionals = %q", pos)
	}
	if len(flags) != 1 || flags[0] != "-config=a.toml" {
		t.Fatalf("flags = %q", flags)
	}
}

func TestSplitArgsDoubleDashEndsFlags(t *testing.T) {
	defineFlags(t)
	flags, pos := splitArgs([]string{"-verbose", "--", "-not-a-flag"})
	if len(pos) != 1 || pos[0] != "-not-a-flag" {
		t.Fatalf("positionals = %q", pos)
	}
	if len(flags) != 1 {
		t.Fatalf("flags = %q", flags)
	}
}

func TestSplitArgsNoArgs(t *testing.T) {
	defineFlags(t)
	flags, pos := splitArgs(nil)
	if len(flags) != 0 || len(pos) != 0 {
		t.Fatalf("flags = %q positionals = %q", flags, pos)
	}
}

// A lone "-" is a conventional stdin placeholder, not a flag.
func TestSplitArgsLoneDashIsPositional(t *testing.T) {
	defineFlags(t)
	_, pos := splitArgs([]string{"-"})
	if len(pos) != 1 || pos[0] != "-" {
		t.Fatalf("positionals = %q", pos)
	}
}

// flagNeedsValue must distinguish string flags from boolean flags; the flag
// package exposes no direct predicate, so the dynamic type is what identifies
// them.
func TestFlagNeedsValue(t *testing.T) {
	defineFlags(t)
	if !flagNeedsValue("-config") || !flagNeedsValue("--config") {
		t.Error("string flag reported as not taking a value")
	}
	if flagNeedsValue("-verbose") {
		t.Error("bool flag reported as taking a value")
	}
	if flagNeedsValue("-nonexistent") {
		t.Error("unknown flag reported as taking a value")
	}
}

// resolveDir must reject a missing or non-directory path with a message naming
// the problem, because the terminal has already taken over the screen by the
// time the shell would notice.
func TestResolveDirRejectsBadPaths(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveDir(dir)
	if err != nil {
		t.Fatalf("resolveDir(%q) = %v", dir, err)
	}
	abs, _ := filepath.Abs(dir)
	if got != abs {
		t.Errorf("resolveDir = %q, want %q", got, abs)
	}

	if _, err := resolveDir(filepath.Join(dir, "nope")); err == nil {
		t.Error("missing directory accepted")
	}
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// The error must name the offending path and the specific problem, or the
	// user sees a bare "invalid" with nothing to act on.
	_, err = resolveDir(file)
	if err == nil {
		t.Fatal("a file accepted as a directory")
	}
	if !strings.Contains(err.Error(), file) || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error = %q, want it to name the path and the problem", err)
	}
}

// An empty directory is not an error: it means "inherit", and must leave the
// config value alone.
func TestResolveDirEmptyIsUnset(t *testing.T) {
	got, err := resolveDir("")
	if err != nil {
		t.Fatalf("resolveDir(\"\") = %v", err)
	}
	if got != "" {
		t.Errorf("resolveDir(\"\") = %q, want empty", got)
	}
}
