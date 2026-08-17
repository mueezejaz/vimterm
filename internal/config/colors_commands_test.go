package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadColorsAndCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[colors]
status_fg = "#ff00aa"
status_bg = "#123456"

[commands]
clean = "leader+c"
up = "k"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Colors.StatusFg != "#ff00aa" || cfg.Colors.StatusBg != "#123456" {
		t.Errorf("colors = %+v", cfg.Colors)
	}
	if cfg.Commands["clean"] != "leader+c" || cfg.Commands["up"] != "k" {
		t.Errorf("commands = %+v", cfg.Commands)
	}
}

func TestLoadDefaultsForEmptyColorsAndCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Colors.StatusFg != "" || cfg.Colors.StatusBg != "" {
		t.Errorf("colors = %+v, want empty", cfg.Colors)
	}
	if cfg.Commands == nil || len(cfg.Commands) != 0 {
		t.Errorf("commands = %+v, want empty map", cfg.Commands)
	}
}

func TestParseHexColor(t *testing.T) {
	c, ok := ParseHexColor("#ff00aa")
	if !ok {
		t.Fatal("ParseHexColor(#ff00aa) not ok")
	}
	if c.R != 0xff || c.G != 0x00 || c.B != 0xaa || c.A != 0xff {
		t.Errorf("color = %+v", c)
	}
	if _, ok := ParseHexColor(""); ok {
		t.Error("empty string must be invalid")
	}
	if _, ok := ParseHexColor("#12345"); ok {
		t.Error("5-digit hex must be invalid")
	}
	if _, ok := ParseHexColor("#gggggg"); ok {
		t.Error("non-hex must be invalid")
	}
	if c, ok := ParseHexColor("123456"); !ok || c.R != 0x12 {
		t.Error("bare hex without # should also parse")
	}
	if _, ok := ParseHexColor("  #ff0000  "); !ok {
		t.Error("whitespace around hex must be tolerated")
	}
}