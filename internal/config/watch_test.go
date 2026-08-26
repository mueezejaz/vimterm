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
	if cfg.Keybindings.Normal["h"] == nil || cfg.Keybindings.Normal["h"][0] != "move_left" {
		t.Errorf("normal h = %q", cfg.Keybindings.Normal["h"])
	}
	if cfg.Keybindings.Insert["esc"] == nil || cfg.Keybindings.Insert["esc"][0] != "enter_normal" {
		t.Errorf("insert esc = %q", cfg.Keybindings.Insert["esc"])
	}
}

func TestLoadChainBinding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `[keybindings.normal]
"leader+nt" = ["new_tab", "rename_prompt"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Keybindings.Normal["leader+nt"]
	if len(got) != 2 || got[0] != "new_tab" || got[1] != "rename_prompt" {
		t.Fatalf("chain = %q, want [new_tab rename_prompt]", got)
	}
}

func TestLoadChainBindingRejectsNonString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[keybindings.normal]\n\"x\" = [\"quit\", 3]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for non-string chain element")
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
	if cfg.Keybindings.Normal["j"] == nil || cfg.Keybindings.Normal["j"][0] != "move_down" {
		t.Errorf("default j binding missing: %q", cfg.Keybindings.Normal["j"])
	}
	if cfg.Keybindings.Insert["esc"] == nil || cfg.Keybindings.Insert["esc"][0] != "enter_normal" {
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
	if cfg.Keybindings.Normal["h"] == nil || cfg.Keybindings.Normal["h"][0] != "move_left" {
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
		if cfg.Keybindings.Normal["h"] == nil || cfg.Keybindings.Normal["h"][0] != "quit" {
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

// A freshly started watcher must not report the file it was just seeded
// from: the zero baselines used to fire a spurious "config reloaded" on
// the first tick of every run.
func TestWatchNoSpuriousInitialReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[general]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := make(chan struct{}, 4)
	stop := Watch(path, 50*time.Millisecond, func(*Config, error) {
		calls <- struct{}{}
	})
	defer stop()
	// Two ticks without any edit: neither may invoke the callback.
	select {
	case <-calls:
		t.Fatal("watcher fired without a config change")
	case <-time.After(150 * time.Millisecond):
	}
}

// A file observed mid-write must not reach the callback: Load used to run
// against the truncated state and hand back a defaults-flavored config as
// if it were valid.
func TestWatchSkipsTornRead(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-sensitive")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	full := []byte("[keybindings.normal]\n\"h\" = \"quit\"\n")
	if err := os.WriteFile(path, full, 0o644); err != nil {
		t.Fatal(err)
	}

	type result struct {
		cfg *Config
		err error
	}
	results := make(chan result, 4)
	stop := Watch(path, 30*time.Millisecond, func(cfg *Config, err error) {
		results <- result{cfg, err}
	})
	defer stop()

	// Truncate, then complete the write while a poll is likely in flight.
	if err := os.WriteFile(path, full[:0], 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(35 * time.Millisecond)
	if err := os.WriteFile(path, full, 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case r := <-results:
			// Any delivered config must be the finished file, never the
			// truncated intermediate (which would parse to defaults).
			if r.err == nil && len(r.cfg.Keybindings.Normal["h"]) > 0 && r.cfg.Keybindings.Normal["h"][0] != "quit" {
				t.Fatalf("torn read delivered h = %q", r.cfg.Keybindings.Normal["h"])
			}
		case <-deadline:
			return // final write already consumed by an earlier tick
		}
	}
}
