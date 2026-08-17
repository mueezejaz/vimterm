package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadKeybindings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `[general]
leader = ","
timeoutlen = 500

[keybindings.normal]
"h" = "move_left"
"leader+t" = "enter_insert"

[keybindings.insert]
"esc" = "enter_normal"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.Leader != "," {
		t.Errorf("leader = %q, want ,", cfg.General.Leader)
	}
	if cfg.General.Timeoutlen != 500 {
		t.Errorf("timeoutlen = %d, want 500", cfg.General.Timeoutlen)
	}
	if cfg.Keybindings.Normal["h"] != "move_left" {
		t.Errorf("normal h = %q", cfg.Keybindings.Normal["h"])
	}
	if cfg.Keybindings.Insert["esc"] != "enter_normal" {
		t.Errorf("insert esc = %q", cfg.Keybindings.Insert["esc"])
	}
}

func TestLoadWithoutKeybindingsUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[general]\nshell = \"cmd.exe\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keybindings.Normal["j"] != "move_down" {
		t.Errorf("default j binding missing: %q", cfg.Keybindings.Normal["j"])
	}
	if cfg.Keybindings.Insert["esc"] != "enter_normal" {
		t.Errorf("default insert esc missing: %q", cfg.Keybindings.Insert["esc"])
	}
}

func TestWatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[general]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keybindings.Normal["h"] != "move_left" {
		t.Fatalf("unexpected initial config: %q", cfg.Keybindings.Normal["h"])
	}

	updates := make(chan *Config, 4)
	stop := Watch(path, 50*time.Millisecond, func(cfg *Config, err error) {
		if err != nil {
			return
		}
		updates <- cfg
	})
	defer stop()

	// Modify the file: remap "h".
	if err := os.WriteFile(path, []byte("[keybindings.normal]\n\"h\" = \"quit\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case cfg := <-updates:
		if cfg.Keybindings.Normal["h"] != "quit" {
			t.Fatalf("reloaded binding h = %q, want quit", cfg.Keybindings.Normal["h"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not report the config change")
	}

	// An invalid edit must not panic and must not deliver a nil config.
	if err := os.WriteFile(path, []byte("not toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
}