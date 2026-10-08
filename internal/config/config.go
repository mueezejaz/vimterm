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

// CursorTrail configures the cursor trail (smear) effect.
type CursorTrail struct {
	// Enabled toggles the cursor trail effect.
	Enabled *bool `toml:"enabled"`
	// Duration is how long each ghost cursor lives in milliseconds.
	Duration *int `toml:"duration"`
	// Opacity is the maximum opacity of the newest ghost (0.0-1.0).
	Opacity *float64 `toml:"opacity"`
	// MaxPositions is the initial ring buffer capacity for trail positions.
	MaxPositions *int `toml:"max_positions"`
	// Easing shapes the fade and the jump sweep: linear, ease_in, ease_out
	// or ease_in_out.
	Easing *string `toml:"easing"`
	// Color is the trail dot color as "#RRGGBB". Empty or absent means
	// use the terminal's default foreground color.
	Color *string `toml:"color"`
	// Glow controls the trail glow intensity (0.0-1.0). 0 means no glow,
	// 1 means maximum glow. Glow brightens the trail dots by blending
	// toward white.
	Glow *float64 `toml:"glow"`
}

// Palette holds the 16 ANSI colors. vimterm installs them into the host
// console's color table on startup and restores the original table on exit,
// so a palette set here changes how the child's colored output looks. An
// empty entry means "leave the host console's own color at that index alone".
type Palette struct {
	Black         string `toml:"black"`
	Red           string `toml:"red"`
	Green         string `toml:"green"`
	Yellow        string `toml:"yellow"`
	Blue          string `toml:"blue"`
	Magenta       string `toml:"magenta"`
	Cyan          string `toml:"cyan"`
	White         string `toml:"white"`
	BrightBlack   string `toml:"bright_black"`
	BrightRed     string `toml:"bright_red"`
	BrightGreen   string `toml:"bright_green"`
	BrightYellow  string `toml:"bright_yellow"`
	BrightBlue    string `toml:"bright_blue"`
	BrightMagenta string `toml:"bright_magenta"`
	BrightCyan    string `toml:"bright_cyan"`
	BrightWhite   string `toml:"bright_white"`
}

// Names lists the palette fields in console color-table index order, paired
// with their TOML key. Keeping this next to Palette is what makes
// Fields able to report palette entries generically.
var paletteNames = []struct {
	name string
	get  func(*Palette) string
}{
	{"black", func(p *Palette) string { return p.Black }},
	{"red", func(p *Palette) string { return p.Red }},
	{"green", func(p *Palette) string { return p.Green }},
	{"yellow", func(p *Palette) string { return p.Yellow }},
	{"blue", func(p *Palette) string { return p.Blue }},
	{"magenta", func(p *Palette) string { return p.Magenta }},
	{"cyan", func(p *Palette) string { return p.Cyan }},
	{"white", func(p *Palette) string { return p.White }},
	{"bright_black", func(p *Palette) string { return p.BrightBlack }},
	{"bright_red", func(p *Palette) string { return p.BrightRed }},
	{"bright_green", func(p *Palette) string { return p.BrightGreen }},
	{"bright_yellow", func(p *Palette) string { return p.BrightYellow }},
	{"bright_blue", func(p *Palette) string { return p.BrightBlue }},
	{"bright_magenta", func(p *Palette) string { return p.BrightMagenta }},
	{"bright_cyan", func(p *Palette) string { return p.BrightCyan }},
	{"bright_white", func(p *Palette) string { return p.BrightWhite }},
}

// PaletteNames lists the 16 ANSI palette keys in console color-table index
// order, so callers can zip a parsed palette against the table without
// repeating the order.
var PaletteNames = paletteNameList()

func paletteNameList() []string {
	names := make([]string, len(paletteNames))
	for i, p := range paletteNames {
		names[i] = p.name
	}
	return names
}

// Colors holds user-configurable color overrides. Empty strings mean the
// terminal default.
type Colors struct {
	StatusFg string `toml:"status_fg"`
	StatusBg string `toml:"status_bg"`

	// Fg and Bg are the terminal's own default foreground and background.
	// They are written into the host console's default text attributes, so
	// every cell the child leaves at "terminal default" is painted in them.
	Fg string `toml:"fg"`
	Bg string `toml:"bg"`

	// Selection tints the visual selection background. Unset falls back to
	// the reverse-video attribute, which inherits the host's colors.
	Selection string `toml:"selection"`

	// Search tints search-match backgrounds. Unset falls back to the
	// reverse-video attribute.
	Search string `toml:"search"`

	// Cursor is the virtual cursor block's foreground color, painted over the
	// cell's own background. Unset keeps the cursor as an inversion of the
	// cell's rendered colors.
	Cursor string `toml:"cursor"`

	// Palette overrides the 16 ANSI colors in the host console's color table.
	Palette Palette `toml:"palette"`
}

// ColorField is one named color from the [colors] section.
type ColorField struct {
	// Name is the TOML key, used in validation errors.
	Name string
	// Value is the raw "#rrggbb" string, empty when unset.
	Value string
}

// Fields returns every user-settable color as a name/value list in a stable
// order. Callers validate them uniformly with ParseHexColor instead of
// checking each field by hand, so a new color needs no new validation code.
func (c Colors) Fields() []ColorField {
	fields := []ColorField{
		{"status_fg", c.StatusFg},
		{"status_bg", c.StatusBg},
		{"fg", c.Fg},
		{"bg", c.Bg},
		{"selection", c.Selection},
		{"search", c.Search},
		{"cursor", c.Cursor},
	}
	for _, p := range paletteNames {
		fields = append(fields, ColorField{"palette." + p.name, p.get(&c.Palette)})
	}
	return fields
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
	CursorTrail CursorTrail `toml:"cursor_trail"`
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
		"ctrl+w":    {"close_tab"},
		"$":         {"move_line_end"},
		"^":         {"move_line_beg"},
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
		"$":      {"move_line_end"},
		"^":      {"move_line_beg"},
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
	type probePalette struct {
		Black         *string `toml:"black"`
		Red           *string `toml:"red"`
		Green         *string `toml:"green"`
		Yellow        *string `toml:"yellow"`
		Blue          *string `toml:"blue"`
		Magenta       *string `toml:"magenta"`
		Cyan          *string `toml:"cyan"`
		White         *string `toml:"white"`
		BrightBlack   *string `toml:"bright_black"`
		BrightRed     *string `toml:"bright_red"`
		BrightGreen   *string `toml:"bright_green"`
		BrightYellow  *string `toml:"bright_yellow"`
		BrightBlue    *string `toml:"bright_blue"`
		BrightMagenta *string `toml:"bright_magenta"`
		BrightCyan    *string `toml:"bright_cyan"`
		BrightWhite   *string `toml:"bright_white"`
	}
	type probeColors struct {
		StatusFg  *string      `toml:"status_fg"`
		StatusBg  *string      `toml:"status_bg"`
		Fg        *string      `toml:"fg"`
		Bg        *string      `toml:"bg"`
		Selection *string      `toml:"selection"`
		Search    *string      `toml:"search"`
		Cursor    *string      `toml:"cursor"`
		Palette   probePalette `toml:"palette"`
	}
	type probeCursorTrail struct {
		Enabled      *bool    `toml:"enabled"`
		Duration     *int     `toml:"duration"`
		Opacity      *float64 `toml:"opacity"`
		MaxPositions *int     `toml:"max_positions"`
		Easing       *string  `toml:"easing"`
		Color        *string  `toml:"color"`
		Glow         *float64 `toml:"glow"`
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
		CursorTrail probeCursorTrail   `toml:"cursor_trail"`
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
	if probe.Colors.Fg != nil {
		cfg.Colors.Fg = *probe.Colors.Fg
	}
	if probe.Colors.Bg != nil {
		cfg.Colors.Bg = *probe.Colors.Bg
	}
	if probe.Colors.Selection != nil {
		cfg.Colors.Selection = *probe.Colors.Selection
	}
	if probe.Colors.Search != nil {
		cfg.Colors.Search = *probe.Colors.Search
	}
	if probe.Colors.Cursor != nil {
		cfg.Colors.Cursor = *probe.Colors.Cursor
	}
	// Palette entries merge by index: a partial [colors.palette] keeps the
	// host console's colors for the indices it does not mention.
	{
		p := &probe.Colors.Palette
		merge := func(dst *string, src *string) {
			if src != nil {
				*dst = *src
			}
		}
		merge(&cfg.Colors.Palette.Black, p.Black)
		merge(&cfg.Colors.Palette.Red, p.Red)
		merge(&cfg.Colors.Palette.Green, p.Green)
		merge(&cfg.Colors.Palette.Yellow, p.Yellow)
		merge(&cfg.Colors.Palette.Blue, p.Blue)
		merge(&cfg.Colors.Palette.Magenta, p.Magenta)
		merge(&cfg.Colors.Palette.Cyan, p.Cyan)
		merge(&cfg.Colors.Palette.White, p.White)
		merge(&cfg.Colors.Palette.BrightBlack, p.BrightBlack)
		merge(&cfg.Colors.Palette.BrightRed, p.BrightRed)
		merge(&cfg.Colors.Palette.BrightGreen, p.BrightGreen)
		merge(&cfg.Colors.Palette.BrightYellow, p.BrightYellow)
		merge(&cfg.Colors.Palette.BrightBlue, p.BrightBlue)
		merge(&cfg.Colors.Palette.BrightMagenta, p.BrightMagenta)
		merge(&cfg.Colors.Palette.BrightCyan, p.BrightCyan)
		merge(&cfg.Colors.Palette.BrightWhite, p.BrightWhite)
	}
	if probe.Keybindings.Normal != nil && len(*probe.Keybindings.Normal) > 0 {
		merged := defaultNormalBindings()
		for k, v := range *probe.Keybindings.Normal {
			if len(v) == 1 && v[0] == "" {
				delete(merged, k)
			} else {
				merged[k] = v
			}
		}
		cfg.Keybindings.Normal = merged
	}
	if probe.Keybindings.Insert != nil && len(*probe.Keybindings.Insert) > 0 {
		merged := defaultInsertBindings()
		for k, v := range *probe.Keybindings.Insert {
			if len(v) == 1 && v[0] == "" {
				delete(merged, k)
			} else {
				merged[k] = v
			}
		}
		cfg.Keybindings.Insert = merged
	}
	if probe.Keybindings.Visual != nil && len(*probe.Keybindings.Visual) > 0 {
		merged := defaultVisualBindings()
		for k, v := range *probe.Keybindings.Visual {
			if len(v) == 1 && v[0] == "" {
				delete(merged, k)
			} else {
				merged[k] = v
			}
		}
		cfg.Keybindings.Visual = merged
	}
	if probe.Commands != nil {
		cfg.Commands = *probe.Commands
	}
	if probe.CursorTrail.Enabled != nil {
		cfg.CursorTrail.Enabled = probe.CursorTrail.Enabled
	}
	if probe.CursorTrail.Duration != nil {
		cfg.CursorTrail.Duration = probe.CursorTrail.Duration
	}
	if probe.CursorTrail.Opacity != nil {
		cfg.CursorTrail.Opacity = probe.CursorTrail.Opacity
	}
	if probe.CursorTrail.MaxPositions != nil {
		cfg.CursorTrail.MaxPositions = probe.CursorTrail.MaxPositions
	}
	if probe.CursorTrail.Easing != nil {
		cfg.CursorTrail.Easing = probe.CursorTrail.Easing
	}
	if probe.CursorTrail.Color != nil {
		cfg.CursorTrail.Color = probe.CursorTrail.Color
	}
	if probe.CursorTrail.Glow != nil {
		cfg.CursorTrail.Glow = probe.CursorTrail.Glow
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
	// Cursor trail defaults and validation.
	if cfg.CursorTrail.Enabled == nil {
		b := false // disabled by default
		cfg.CursorTrail.Enabled = &b
	}
	if cfg.CursorTrail.Duration == nil {
		d := 300
		cfg.CursorTrail.Duration = &d
	} else if *cfg.CursorTrail.Duration < 0 {
		d := 0
		cfg.CursorTrail.Duration = &d
	}
	if cfg.CursorTrail.Opacity == nil {
		o := 0.6
		cfg.CursorTrail.Opacity = &o
	} else if *cfg.CursorTrail.Opacity < 0 {
		o := 0.0
		cfg.CursorTrail.Opacity = &o
	} else if *cfg.CursorTrail.Opacity > 1 {
		o := 1.0
		cfg.CursorTrail.Opacity = &o
	}
	if cfg.CursorTrail.MaxPositions == nil {
		m := 40
		cfg.CursorTrail.MaxPositions = &m
	} else if *cfg.CursorTrail.MaxPositions < 4 {
		m := 4
		cfg.CursorTrail.MaxPositions = &m
	}
	if cfg.CursorTrail.Easing == nil {
		e := "linear"
		cfg.CursorTrail.Easing = &e
	} else {
		switch *cfg.CursorTrail.Easing {
		case "linear", "ease_in", "ease_out", "ease_in_out":
		default:
			return nil, fmt.Errorf("config: cursor_trail: easing: invalid value %q (want linear, ease_in, ease_out or ease_in_out)", *cfg.CursorTrail.Easing)
		}
	}
	if cfg.CursorTrail.Color != nil && *cfg.CursorTrail.Color != "" {
		if _, ok := ParseHexColor(*cfg.CursorTrail.Color); !ok {
			return nil, fmt.Errorf("config: cursor_trail: color: invalid color %q (want #RRGGBB)", *cfg.CursorTrail.Color)
		}
	}
	if cfg.CursorTrail.Glow == nil {
		g := 0.0
		cfg.CursorTrail.Glow = &g
	} else if *cfg.CursorTrail.Glow < 0 {
		g := 0.0
		cfg.CursorTrail.Glow = &g
	} else if *cfg.CursorTrail.Glow > 1 {
		g := 1.0
		cfg.CursorTrail.Glow = &g
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

# Terminal colors. Everything here is optional: an empty value keeps whatever
# the host terminal already uses, so a config that sets nothing looks exactly
# like vimterm has always looked.
#
# fg / bg          the terminal's default foreground and background
# selection        visual selection background (default: reverse video)
# search           search-match background (default: reverse video)
# cursor           virtual cursor block foreground (default: inverted cell)
fg = ""
bg = ""
selection = ""
search = ""
cursor = ""

# The 16 ANSI colors. vimterm installs these into the host console's color
# table on startup and restores the original table on exit, so this is what
# decides how your shell's colored output looks. Set only the entries you care
# about; the rest keep the host terminal's colors.
[colors.palette]
# black = "#1c1c1c"
# red = "#e05561"
# green = "#8cc265"
# yellow = "#d18f52"
# blue = "#4aa5f0"
# magenta = "#c162de"
# cyan = "#42b3c2"
# white = "#d7dae0"
# bright_black = "#5d6673"
# bright_red = "#ff616e"
# bright_green = "#a5e075"
# bright_yellow = "#f0a45d"
# bright_blue = "#4dc4ff"
# bright_magenta = "#de73ff"
# bright_cyan = "#4cd1e0"
# bright_white = "#e6e6e6"

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
"ctrl+w" = "close_tab"
"$" = "move_line_end"

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
"$" = "move_line_end"
# Disabled by default; enable and tune to your taste.
# [cursor_trail]
# enabled = true
# duration = 300       # ms each ghost lives
# opacity = 0.6        # max opacity of newest ghost (0.0-1.0)
# max_positions = 40   # initial ring capacity (grows to fit long jumps)
# easing = "linear"    # linear, ease_in, ease_out or ease_in_out
`
