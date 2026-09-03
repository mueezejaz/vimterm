package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.General.Shell != "powershell.exe" {
		t.Errorf("default shell = %q, want powershell.exe", cfg.General.Shell)
	}
	if cfg.General.Scrollback != 10000 {
		t.Errorf("default scrollback = %d, want 10000", cfg.General.Scrollback)
	}
	if cfg.General.StatusMerge != "auto" {
		t.Errorf("default status_merge = %q, want auto", cfg.General.StatusMerge)
	}
}

func TestLoadStatusMerge(t *testing.T) {
	for _, want := range []string{"auto", "always", "never"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte("[general]\nstatus_merge = \""+want+"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("status_merge = %q: %v", want, err)
		}
		if cfg.General.StatusMerge != want {
			t.Errorf("status_merge = %q, want %q", cfg.General.StatusMerge, want)
		}
	}
}

func TestLoadInvalidStatusMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[general]\nstatus_merge = \"sometimes\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid status_merge")
	}
}

func TestLoadCursorTrailEasing(t *testing.T) {
	for _, want := range []string{"linear", "ease_in", "ease_out", "ease_in_out"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte("[cursor_trail]\nenabled = true\neasing = \""+want+"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("easing = %q: %v", want, err)
		}
		if cfg.CursorTrail.Easing == nil || *cfg.CursorTrail.Easing != want {
			t.Errorf("easing = %v, want %q", cfg.CursorTrail.Easing, want)
		}
	}
}

func TestLoadCursorTrailEasingDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[cursor_trail]\nenabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CursorTrail.Easing == nil || *cfg.CursorTrail.Easing != "linear" {
		t.Errorf("default easing = %v, want linear", cfg.CursorTrail.Easing)
	}
}

func TestLoadInvalidCursorTrailEasing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[cursor_trail]\neasing = \"bounce\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid cursor_trail easing")
	}
}

func TestLoadMissingKeysFallBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[general]\nshell = \"cmd.exe\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.Shell != "cmd.exe" {
		t.Errorf("shell = %q, want cmd.exe", cfg.General.Shell)
	}
	if cfg.General.Scrollback != 10000 {
		t.Errorf("scrollback = %d, want default 10000", cfg.General.Scrollback)
	}
}

func TestLoadInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("this is { not toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestEnsureDefaultCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vimterm", "config.toml")
	if err := EnsureDefault(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("default config not created: %v", err)
	}
	// Second call must be a no-op.
	if err := EnsureDefault(path); err != nil {
		t.Fatal(err)
	}
}

func TestLoadEmptyShellFallsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[general]\nshell = \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.Shell != "powershell.exe" {
		t.Errorf("shell = %q, want fallback powershell.exe", cfg.General.Shell)
	}
}
