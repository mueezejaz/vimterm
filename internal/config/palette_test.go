package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadBody(t *testing.T, body string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// The whole point of the palette feature: a user can describe a full color
// scheme in [colors] and every entry has to survive the probe-merge, which is
// where a new key is most easily forgotten.
func TestLoadFullColorScheme(t *testing.T) {
	cfg := loadBody(t, `
[colors]
fg = "#101010"
bg = "#fefefe"
selection = "#264f78"
search = "#5f5f00"
cursor = "#00ff87"
status_fg = "#eeeeee"
status_bg = "#202020"

[colors.palette]
black = "#101010"
red = "#e05561"
green = "#8cc265"
yellow = "#d18f52"
blue = "#4aa5f0"
magenta = "#c162de"
cyan = "#42b3c2"
white = "#d7dae0"
bright_black = "#5d6673"
bright_red = "#ff616e"
bright_green = "#a5e075"
bright_yellow = "#f0a45d"
bright_blue = "#4dc4ff"
bright_magenta = "#de73ff"
bright_cyan = "#4cd1e0"
bright_white = "#e6e6e6"
`)
	c := cfg.Colors
	if c.Fg != "#101010" || c.Bg != "#fefefe" {
		t.Errorf("fg/bg = %q/%q", c.Fg, c.Bg)
	}
	if c.Selection != "#264f78" || c.Search != "#5f5f00" || c.Cursor != "#00ff87" {
		t.Errorf("highlights = %q/%q/%q", c.Selection, c.Search, c.Cursor)
	}
	p := c.Palette
	got := []string{
		p.Black, p.Red, p.Green, p.Yellow, p.Blue, p.Magenta, p.Cyan, p.White,
		p.BrightBlack, p.BrightRed, p.BrightGreen, p.BrightYellow,
		p.BrightBlue, p.BrightMagenta, p.BrightCyan, p.BrightWhite,
	}
	for i, v := range got {
		if v == "" {
			t.Errorf("palette entry %d (%s) not loaded", i, PaletteNames[i])
		}
	}
}

// A partial [colors.palette] must not zero the entries it does not mention:
// the user is overriding a few indices of the host's table, not replacing it.
func TestLoadPartialPaletteKeepsOtherEntriesEmpty(t *testing.T) {
	cfg := loadBody(t, `
[colors.palette]
red = "#ff0000"
bright_blue = "#0000ff"
`)
	p := cfg.Colors.Palette
	if p.Red != "#ff0000" || p.BrightBlue != "#0000ff" {
		t.Errorf("set entries = %q/%q", p.Red, p.BrightBlue)
	}
	if p.Black != "" || p.BrightWhite != "" {
		t.Errorf("unset entries should stay empty, got black=%q bright_white=%q", p.Black, p.BrightWhite)
	}
}

// An empty config must leave every color unset, so vimterm keeps inheriting
// the host terminal's palette.
func TestLoadEmptyColorsLeavesEverythingUnset(t *testing.T) {
	cfg := loadBody(t, "")
	for _, f := range cfg.Colors.Fields() {
		if f.Value != "" {
			t.Errorf("color %s should default to unset, got %q", f.Name, f.Value)
		}
	}
}

// Fields is the single validation surface for [colors]; it must expose every
// settable color exactly once, in color-table order for the palette.
// [general] dir and env are how a user controls where and how the shell starts;
// both must survive the probe-merge that every new key has to be threaded
// through.
func TestLoadGeneralDirAndEnv(t *testing.T) {
	cfg := loadBody(t, `
[general]
dir = 'C:\Users\me\projects'
env = { EDITOR = "code -w", MYVAR = "x" }
`)
	if cfg.General.Dir != `C:\Users\me\projects` {
		t.Errorf("dir = %q", cfg.General.Dir)
	}
	if cfg.General.Env["EDITOR"] != "code -w" || cfg.General.Env["MYVAR"] != "x" {
		t.Errorf("env = %+v", cfg.General.Env)
	}
}

// dir and env must default to unset so the shell inherits vimterm's working
// directory and environment exactly as before.
func TestLoadGeneralDirAndEnvDefaultUnset(t *testing.T) {
	cfg := loadBody(t, "")
	if cfg.General.Dir != "" {
		t.Errorf("dir = %q, want empty", cfg.General.Dir)
	}
	if len(cfg.General.Env) != 0 {
		t.Errorf("env = %+v, want empty", cfg.General.Env)
	}
}

// An empty env variable name would produce a malformed "=value" entry in the
// child environment, so it is rejected rather than silently dropped.
func TestLoadGeneralEnvRejectsEmptyName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[general]\nenv = { \"\" = \"x\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an empty env variable name")
	}
}

// The generated default config is where users discover these options.
func TestDefaultTomlDocumentsDirAndEnv(t *testing.T) {
	for _, key := range []string{"dir", "env"} {
		if !containsLine(defaultToml, key+" = ") && !containsLine(defaultToml, "# "+key+" = ") {
			t.Errorf("defaultToml does not document general.%s", key)
		}
	}
}

func TestColorsFieldsCoversEveryColor(t *testing.T) {
	names := map[string]bool{}
	all := Colors{}.Fields()
	for _, f := range all {
		if names[f.Name] {
			t.Errorf("duplicate color field %q", f.Name)
		}
		names[f.Name] = true
	}
	for _, want := range []string{"status_fg", "status_bg", "fg", "bg", "selection", "search", "cursor"} {
		if !names[want] {
			t.Errorf("Fields() is missing %q", want)
		}
	}
	if len(PaletteNames) != 16 {
		t.Fatalf("PaletteNames has %d entries, want 16", len(PaletteNames))
	}
	for _, name := range PaletteNames {
		if !names["palette."+name] {
			t.Errorf("Fields() is missing palette entry %q", name)
		}
	}
	if got := len(all); got != 7+16 {
		t.Errorf("Fields() returned %d entries, want %d", got, 7+16)
	}
}

// The generated default config is what users read to learn the options, so
// every color key has to appear in it.
func TestDefaultTomlDocumentsEveryColor(t *testing.T) {
	fields := Colors{}.Fields()
	for _, f := range fields {
		if f.Name == "status_fg" || f.Name == "status_bg" {
			continue // already present as live (uncommented) keys
		}
		// Palette entries live inside the [colors.palette] table, so the key
		// as written in the file is the bare name.
		key := f.Name
		if after, ok := strings.CutPrefix(key, "palette."); ok {
			key = after
		}
		if !containsLine(defaultToml, key+" = ") && !containsLine(defaultToml, "# "+key+" = ") {
			t.Errorf("defaultToml does not document color %q", f.Name)
		}
	}
}

// containsLine reports whether s contains the substring line.
func containsLine(s, line string) bool {
	for i := 0; i+len(line) <= len(s); i++ {
		if s[i:i+len(line)] == line {
			return true
		}
	}
	return false
}
