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
	// StatusMerge controls whether full-screen applications (alternate
	// screen, e.g. nvim) get the full terminal height with vimterm's status
	// bar overlaid on their status line: "auto" (merge only when the
	// bottom row looks like a status line), "always", or "never".
	StatusMerge string `toml:"status_merge"`
}

// Binding is one key sequence's payload: a single action name, or an
// ordered chain of several that the application runs in sequence on a
// match. In TOML both spellings are accepted:
//
//	"h" = "move_left"
//	"leader+nt" = ["new_tab", "rename_prompt"]
type Binding []string

// UnmarshalTOML implements toml.Unmarshaler so a binding may be written as
// either a string or a list of strings. go-toml hands over the raw TOML
// bytes of the value; they are re-parsed generically to accept both shapes.
func (b *Binding) UnmarshalTOML(data []byte) error {
	// The raw bytes are a bare TOML *value* ("x" or ["a", "b"]), not a
	// document, so embed them in a one-line document before decoding.
	wrapped := append([]byte("binding = "), data...)
	wrapped = append(wrapped, '\n')
	var wrapper struct {
		Binding any
	}
	if err := toml.Unmarshal(wrapped, &wrapper); err != nil {
		return err
	}
	switch t := wrapper.Binding.(type) {
	case string:
		*b = Binding{t}
		return nil
	case []interface{}:
		out := make(Binding, 0, len(t))
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				return fmt.Errorf("config: action chains must contain only strings, got %T", item)
			}
			out = append(out, s)
		}
		*b = out
		return nil
	default:
		return fmt.Errorf("config: binding must be a string or list of strings, got %T", wrapper.Binding)
	}
}

// Keybindings maps mode names to binding tables. Each table maps a key
// sequence token (e.g. "gg", "ctrl+u", "leader+t") to one action name or an
// ordered chain of action names.
type Keybindings struct {
	Normal map[string]Binding `toml:"normal"`
	Insert map[string]Binding `toml:"insert"`
	Visual map[string]Binding `toml:"visual"`
}

// ActionTables flattens every mode's bindings into plain action-name lists,
// ready for keybind.BuildKeymaps.
func (kb *Keybindings) ActionTables() map[string]map[string][]string {
	tables := make(map[string]map[string][]string, 3)
	for modeName, table := range map[string]map[string]Binding{
		"normal": kb.Normal,
		"insert": kb.Insert,
		"visual": kb.Visual,
	} {
		out := make(map[string][]string, len(table))
		for token, b := range table {
			out[token] = []string(b)
		}
		tables[modeName] = out
	}
	return tables
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
	General     General     `toml:"general"`
	Keybindings Keybindings `toml:"keybindings"`
	Colors      Colors      `toml:"colors"`
	Commands    Commands    `toml:"commands"`
}

// Default returns the built-in defaults.
func Default() *Config {
	return &Config{
		General: General{
			Shell:       "powershell.exe",
			ShellArgs:   []string{},
			Scrollback:  10000,
			Leader:      "space",
			Timeoutlen:  1000,
			StatusMerge: "auto",
		},
		Keybindings: Keybindings{
			Normal: defaultNormalBindings(),
			Insert: defaultInsertBindings(),
			Visual: defaultVisualBindings(),
		},
		Commands: Commands{},
	}
}

func defaultNormalBindings() map[string]Binding {
	return map[string]Binding{
		"h":         {"move_left"},
		"j":         {"move_down"},
		"k":         {"move_up"},
		"l":         {"move_right"},
		"left":      {"move_left"},
		"down":      {"move_down"},
		"up":        {"move_up"},
		"right":     {"move_right"},
		"gg":        {"goto_top"},
		"G":         {"goto_bottom"},
		"ctrl+u":    {"scroll_up"},
		"ctrl+d":    {"scroll_down"},
		"i":         {"enter_insert"},
		"a":         {"enter_insert_after"},
		"A":         {"enter_insert_end"},
		"I":         {"enter_insert_home"},
		"/":         {"search_forward"},
		"n":         {"search_next"},
		"N":         {"search_prev"},
		":":         {"command_prompt"},
		"v":         {"enter_visual"},
		"V":         {"enter_visual_line"},
		"y":         {"yank"},
		"yy":        {"yank_line"},
		"dw":        {"delete_word"},
		"db":        {"delete_word_back"},
		"p":         {"paste"},
		"P":         {"paste_before"},
		"q":         {"record_macro"},
		"@":         {"play_macro"},
		".":         {"repeat_last"},
		"f":         {"find_char"},
		"F":         {"find_char_back"},
		"t":         {"find_until"},
		"T":         {"find_until_back"},
		";":         {"find_next"},
		",":         {"find_prev"},
		"w":         {"move_word"},
		"b":         {"move_word_back"},
		"e":         {"move_word_end"},
		"W":         {"move_word_upper"},
		"B":         {"move_word_back_upper"},
		"E":         {"move_word_end_upper"},
		"gt":        {"next_tab"},
		"gT":        {"prev_tab"},
		"leader+nt": {"new_tab", "rename_prompt"},
		"leader+tt": {"tab_search"},
		"ctrl+q":    {"quit"},
	}
}

func defaultInsertBindings() map[string]Binding {
	return map[string]Binding{
		"esc":    {"enter_normal"},
		"ctrl+q": {"quit"},
	}
}

func defaultVisualBindings() map[string]Binding {
	return map[string]Binding{
		"h":      {"move_left"},
		"j":      {"move_down"},
		"k":      {"move_up"},
		"l":      {"move_right"},
		"left":   {"move_left"},
		"down":   {"move_down"},
		"up":     {"move_up"},
		"right":  {"move_right"},
		"gg":     {"goto_top"},
		"G":      {"goto_bottom"},
		"ctrl+u": {"scroll_up"},
		"ctrl+d": {"scroll_down"},
		"v":      {"enter_visual"},
		"V":      {"enter_visual_line"},
		"y":      {"yank"},
		"d":      {"yank"},
		"p":      {"paste"},
		"P":      {"paste_before"},
		"f":      {"find_char"},
		"F":      {"find_char_back"},
		"t":      {"find_until"},
		"T":      {"find_until_back"},
		";":      {"find_next"},
		",":      {"find_prev"},
		"w":      {"move_word"},
		"b":      {"move_word_back"},
		"e":      {"move_word_end"},
		"W":      {"move_word_upper"},
		"B":      {"move_word_back_upper"},
		"E":      {"move_word_end_upper"},
		"i":      {"enter_insert"},
		"esc":    {"enter_normal"},
		"ctrl+q": {"quit"},
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
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer f.Close()

	// Decode into a parallel struct with pointer fields. TOML merges into
	// existing maps, so decoding directly into Default() would keep default
	// keys the user never listed (e.g. "esc" in insert mode). Pointer
	// fields let us distinguish "section present in TOML" from "absent"
	// and only apply defaults for truly absent ones.
	type probeGeneral struct {
		Shell       *string  `toml:"shell"`
		ShellArgs   []string `toml:"shell_args"`
		Scrollback  *int     `toml:"scrollback"`
		Leader      *string  `toml:"leader"`
		Timeoutlen  *int     `toml:"timeoutlen"`
		StatusMerge *string  `toml:"status_merge"`
	}
	type probeColors struct {
		StatusFg *string `toml:"status_fg"`
		StatusBg *string `toml:"status_bg"`
	}
	type probeKeybindings struct {
		Normal *map[string]Binding `toml:"normal"`
		Insert *map[string]Binding `toml:"insert"`
		Visual *map[string]Binding `toml:"visual"`
	}
	type probeConfig struct {
		General     probeGeneral       `toml:"general"`
		Keybindings probeKeybindings   `toml:"keybindings"`
		Colors      probeColors        `toml:"colors"`
		Commands    *map[string]string `toml:"commands"`
	}
	var probe probeConfig

	dec := toml.NewDecoder(f).EnableUnmarshalerInterface()
	if err := dec.Decode(&probe); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	// Merge probe into cfg: only overwrite defaults when the TOML provided
	// a value (non-nil pointer means "key was present in TOML").
	if probe.General.Shell != nil {
		cfg.General.Shell = *probe.General.Shell
	}
	if probe.General.ShellArgs != nil {
		cfg.General.ShellArgs = probe.General.ShellArgs
	}
	if probe.General.Scrollback != nil {
		cfg.General.Scrollback = *probe.General.Scrollback
	}
	if probe.General.Leader != nil {
		cfg.General.Leader = *probe.General.Leader
	}
	if probe.General.Timeoutlen != nil {
		cfg.General.Timeoutlen = *probe.General.Timeoutlen
	}
	if probe.General.StatusMerge != nil {
		cfg.General.StatusMerge = *probe.General.StatusMerge
	}
	if probe.Colors.StatusFg != nil {
		cfg.Colors.StatusFg = *probe.Colors.StatusFg
	}
	if probe.Colors.StatusBg != nil {
		cfg.Colors.StatusBg = *probe.Colors.StatusBg
	}
	if probe.Keybindings.Normal != nil {
		cfg.Keybindings.Normal = *probe.Keybindings.Normal
	}
	if probe.Keybindings.Insert != nil {
		cfg.Keybindings.Insert = *probe.Keybindings.Insert
	}
	if probe.Keybindings.Visual != nil {
		cfg.Keybindings.Visual = *probe.Keybindings.Visual
	}
	if probe.Commands != nil {
		cfg.Commands = *probe.Commands
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
	switch cfg.General.StatusMerge {
	case "", "auto", "always", "never":
		if cfg.General.StatusMerge == "" {
			cfg.General.StatusMerge = "auto"
		}
	default:
		return nil, fmt.Errorf("config: general: status_merge: invalid value %q (want auto, always or never)", cfg.General.StatusMerge)
	}
	// TOML cannot distinguish an absent table from an empty one; treat
	// absent sections as "use the defaults" so a minimal config keeps
	// its bindings.
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
# Full-screen applications (alternate screen, e.g. nvim) take the full
# terminal height and vimterm's status bar overlays their status line while a
# transient message is shown. "auto" merges only when the bottom row looks
# like a status line, "always" merges unconditionally, "never" keeps the
# always-visible vimterm bar.
status_merge = "auto"

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
"a" = "enter_insert_after"
"A" = "enter_insert_end"
"I" = "enter_insert_home"
"/" = "search_forward"
"n" = "search_next"
"N" = "search_prev"
":" = "command_prompt"
"v" = "enter_visual"
"V" = "enter_visual_line"
"yy" = "yank_line"
"dw" = "delete_word"
"db" = "delete_word_back"
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
"gt" = "next_tab"
"gT" = "prev_tab"
# Chains run several actions in order; the chain stops early when a step
# opens a prompt (the prompt takes over input).
"leader+nt" = ["new_tab", "rename_prompt"]
"leader+tt" = "tab_search"
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
"p" = "paste"
"P" = "paste_before"
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
