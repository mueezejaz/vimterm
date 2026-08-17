// Package config loads and validates the TOML configuration.
package config

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// General holds non-mode-specific settings.
type General struct {
	// Shell is the program launched in the PTY (e.g. "powershell.exe").
	Shell string
	// ShellArgs are extra arguments passed to the shell.
	ShellArgs []string
	// Scrollback is the maximum number of scrolled-off lines kept in memory.
	Scrollback int
	// Leader is the config token for the leader key (e.g. "space").
	Leader string
	// Timeoutlen is the time in milliseconds a partial key sequence may
	// wait for completion before being discarded.
	Timeoutlen int
}

// Keybindings maps mode names to binding tables. Each table maps a key
// sequence token (e.g. "gg", "ctrl+u", "leader+t") to an action name.
type Keybindings struct {
	Normal map[string]string `toml:"normal"`
	Insert map[string]string `toml:"insert"`
	Visual map[string]string `toml:"visual"`
}

// Colors holds user-configurable color overrides. Empty strings mean the
// terminal default.
type Colors struct {
	StatusFg string `toml:"status_fg"`
	StatusBg string `toml:"status_bg"`
}

// Commands maps custom colon-command names to key sequences (in binding
// token syntax) that are replayed through the keybinding engine.
type Commands map[string]string

// Config is the full application configuration.
type Config struct {
	General    General    `toml:"general"`
	Keybindings Keybindings `toml:"keybindings"`
	Colors     Colors     `toml:"colors"`
	Commands   Commands   `toml:"commands"`
}

// Default returns the built-in defaults.
func Default() *Config {
	return &Config{
		General: General{
			Shell:      "powershell.exe",
			ShellArgs:  []string{},
			Scrollback: 10000,
			Leader:     "space",
			Timeoutlen: 1000,
		},
		Keybindings: Keybindings{
			Normal: defaultNormalBindings(),
			Insert: defaultInsertBindings(),
			Visual: defaultVisualBindings(),
		},
		Commands: Commands{},
	}
}

func defaultNormalBindings() map[string]string {
	return map[string]string{
		"h":      "move_left",
		"j":      "move_down",
		"k":      "move_up",
		"l":      "move_right",
		"left":   "move_left",
		"down":   "move_down",
		"up":     "move_up",
		"right":  "move_right",
		"gg":     "goto_top",
		"G":      "goto_bottom",
		"ctrl+u": "scroll_up",
		"ctrl+d": "scroll_down",
		"i":      "enter_insert",
		"/":      "search_forward",
		"n":      "search_next",
		"N":      "search_prev",
		":":      "command_prompt",
		"v":      "enter_visual",
		"V":      "enter_visual_line",
		"q":      "record_macro",
		"@":      "play_macro",
		".":      "repeat_last",
		"f":      "find_char",
		"F":      "find_char_back",
		"t":      "find_until",
		"T":      "find_until_back",
		";":      "find_next",
		",":      "find_prev",
		"w":      "move_word",
		"b":      "move_word_back",
		"e":      "move_word_end",
		"W":      "move_word_upper",
		"B":      "move_word_back_upper",
		"E":      "move_word_end_upper",
		"ctrl+q": "quit",
	}
}

func defaultInsertBindings() map[string]string {
	return map[string]string{
		"esc":    "enter_normal",
		"ctrl+q": "quit",
	}
}

func defaultVisualBindings() map[string]string {
	return map[string]string{
		"h":      "move_left",
		"j":      "move_down",
		"k":      "move_up",
		"l":      "move_right",
		"left":   "move_left",
		"down":   "move_down",
		"up":     "move_up",
		"right":  "move_right",
		"gg":     "goto_top",
		"G":      "goto_bottom",
		"ctrl+u": "scroll_up",
		"ctrl+d": "scroll_down",
		"v":      "enter_visual",
		"V":      "enter_visual_line",
		"y":      "yank",
		"d":      "yank",
		"f":      "find_char",
		"F":      "find_char_back",
		"t":      "find_until",
		"T":      "find_until_back",
		";":      "find_next",
		",":      "find_prev",
		"w":      "move_word",
		"b":      "move_word_back",
		"e":      "move_word_end",
		"W":      "move_word_upper",
		"B":      "move_word_back_upper",
		"E":      "move_word_end_upper",
		"i":      "enter_insert",
		"esc":    "enter_normal",
		"ctrl+q": "quit",
	}
}

// DefaultPath returns the standard config location on Windows:
// %APPDATA%\vimterm\config.toml
func DefaultPath() string {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		appdata = os.Getenv("LOCALAPPDATA")
	}
	if appdata == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		appdata = home
	}
	return filepath.Join(appdata, "vimterm", "config.toml")
}

// EnsureDefault writes a commented default config file if none exists.
func EnsureDefault(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultToml), 0o644)
}

// Load reads and validates the config file. Missing keys fall back to
// defaults.
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if cfg.General.Shell == "" {
		cfg.General.Shell = "powershell.exe"
	}
	if cfg.General.Scrollback < 0 {
		cfg.General.Scrollback = 0
	}
	if cfg.General.Timeoutlen <= 0 {
		cfg.General.Timeoutlen = 1000
	}
	// TOML cannot distinguish an absent table from an empty one; treat absent
	// sections as "use the defaults" so a minimal config keeps its bindings.
	if cfg.Keybindings.Normal == nil {
		cfg.Keybindings.Normal = defaultNormalBindings()
	}
	if cfg.Keybindings.Insert == nil {
		cfg.Keybindings.Insert = defaultInsertBindings()
	}
	if cfg.Keybindings.Visual == nil {
		cfg.Keybindings.Visual = defaultVisualBindings()
	}
	if cfg.Commands == nil {
		cfg.Commands = Commands{}
	}
	return cfg, nil
}

// ParseHexColor converts a "#rrggbb" string to an RGBA color. It reports
// false for empty or invalid strings.
func ParseHexColor(s string) (color.RGBA, bool) {
	if s == "" {
		return color.RGBA{}, false
	}
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return color.RGBA{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{
		R: uint8(v >> 16),
		G: uint8(v >> 8),
		B: uint8(v),
		A: 0xff,
	}, true
}

const defaultToml = `# vimterm configuration
# Location of this file: %APPDATA%\vimterm\config.toml
# Editing it reloads bindings live (checked once per second).

[general]
# Program launched inside the terminal (PowerShell, cmd.exe, wsl.exe, ...).
shell = "powershell.exe"
# Extra arguments passed to the shell.
shell_args = []
# Maximum number of scrollback lines kept in memory.
scrollback = 10000
# Leader key, usable in bindings as the "leader" token (e.g. "leader+t").
leader = "space"
# Milliseconds a partial key sequence (e.g. the first "g" of "gg") waits for
# the next key before being discarded.
timeoutlen = 1000

[colors]
# Status line colors, "#rrggbb". Empty = terminal defaults.
status_fg = ""
status_bg = ""

[commands]
# Custom colon-commands: a name maps to a key sequence (binding token
# syntax) replayed through the keybinding engine. Example:
#   clean = "leader+c"
# Then ":clean" replays the "leader+c" sequence.

[keybindings.normal]
"h" = "move_left"
"j" = "move_down"
"k" = "move_up"
"l" = "move_right"
"left" = "move_left"
"down" = "move_down"
"up" = "move_up"
"right" = "move_right"
"gg" = "goto_top"
"G" = "goto_bottom"
"ctrl+u" = "scroll_up"
"ctrl+d" = "scroll_down"
"i" = "enter_insert"
"/" = "search_forward"
"n" = "search_next"
"N" = "search_prev"
":" = "command_prompt"
"v" = "enter_visual"
"V" = "enter_visual_line"
"q" = "record_macro"
"@" = "play_macro"
"." = "repeat_last"
"f" = "find_char"
"F" = "find_char_back"
"t" = "find_until"
"T" = "find_until_back"
";" = "find_next"
"," = "find_prev"
"w" = "move_word"
"b" = "move_word_back"
"e" = "move_word_end"
"W" = "move_word_upper"
"B" = "move_word_back_upper"
"E" = "move_word_end_upper"
"ctrl+q" = "quit"

[keybindings.insert]
"esc" = "enter_normal"
"ctrl+q" = "quit"

[keybindings.visual]
"h" = "move_left"
"j" = "move_down"
"k" = "move_up"
"l" = "move_right"
"left" = "move_left"
"down" = "move_down"
"up" = "move_up"
"right" = "move_right"
"gg" = "goto_top"
"G" = "goto_bottom"
"ctrl+u" = "scroll_up"
"ctrl+d" = "scroll_down"
"v" = "enter_visual"
"V" = "enter_visual_line"
"y" = "yank"
"d" = "yank"
"f" = "find_char"
"F" = "find_char_back"
"t" = "find_until"
"T" = "find_until_back"
";" = "find_next"
"," = "find_prev"
"w" = "move_word"
"b" = "move_word_back"
"e" = "move_word_end"
"W" = "move_word_upper"
"B" = "move_word_back_upper"
"E" = "move_word_end_upper"
"i" = "enter_insert"
"esc" = "enter_normal"
"ctrl+q" = "quit"
`