package app

// Tests for the [colors] palette/theme layer: every color is optional, so
// the default path must keep the pre-palette behavior (reverse video for
// highlights, host-inherited defaults for the terminal colors) while a
// configured color must take over.

import (
	"testing"

	"vimterm/internal/config"
	"vimterm/internal/emulator"
)

// withColors returns the default config with the given [colors] overrides.
func withColors(c config.Colors) *config.Config {
	cfg := config.Default()
	cfg.Colors = c
	return cfg
}

func rgb(hex string) emulator.Color {
	c, ok := config.ParseHexColor(hex)
	if !ok {
		panic("bad test color " + hex)
	}
	return emulator.Color{R: c.R, G: c.G, B: c.B}
}

// A config that sets nothing must leave every highlight unset, so the renderer
// keeps falling back to the reverse attribute. The subtle failure this guards
// is storing the zero emulator.Color, which is a valid black and would paint
// every selection black instead of leaving it unstyled.
func TestColorsUnsetByDefault(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	if err := a.applyConfig(config.Default()); err != nil {
		t.Fatal(err)
	}
	if _, set := a.selectionColor(); set {
		t.Error("selection color set with no configuration")
	}
	if _, set := a.searchMatchColor(); set {
		t.Error("search color set with no configuration")
	}
	if _, set := a.cursorBlockColor(); set {
		t.Error("cursor color set with no configuration")
	}
}

// Configured highlights must come back out of the cfgMu-guarded accessors
// exactly as written.
func TestConfiguredHighlightColorsAreStored(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	cfg := withColors(config.Colors{
		Selection: "#264f78",
		Search:    "#5f5f00",
		Cursor:    "#00ff87",
	})
	if err := a.applyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if got, set := a.selectionColor(); !set || got != rgb("#264f78") {
		t.Errorf("selection = %+v set=%v", got, set)
	}
	if got, set := a.searchMatchColor(); !set || got != rgb("#5f5f00") {
		t.Errorf("search = %+v set=%v", got, set)
	}
	if got, set := a.cursorBlockColor(); !set || got != rgb("#00ff87") {
		t.Errorf("cursor = %+v set=%v", got, set)
	}
}

// A palette entry of "#000000" is a real color, not an absent one: the
// Default flag is what distinguishes them, so this must stay "set".
func TestBlackIsNotMistakenForUnset(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	cfg := withColors(config.Colors{Selection: "#000000"})
	if err := a.applyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	got, set := a.selectionColor()
	if !set || got != (emulator.Color{}) {
		t.Errorf("black selection = %+v set=%v, want the zero color but set", got, set)
	}
}

// An invalid color anywhere in [colors] must reject the whole reload: these
// are interpreted per-field, so a half-applied scheme would paint the screen
// with a mix of old and new colors.
func TestInvalidColorIsRejectedForEveryField(t *testing.T) {
	base := config.Default()
	for _, f := range base.Colors.Fields() {
		a := realApp(t, 40, 6, "x\r\n")
		if err := a.applyConfig(config.Default()); err != nil {
			t.Fatal(err)
		}
		before := a.cfg

		cfg := config.Default()
		applyColorOverride(&cfg.Colors, f.Name, "not-a-color")
		err := a.applyConfig(cfg)
		if err == nil {
			t.Errorf("%s: accepted an invalid color", f.Name)
			continue
		}
		if a.cfg != before {
			t.Errorf("%s: rejected config was still committed", f.Name)
		}
	}
}

// applyColorOverride sets one named color on c. Every key is spelled out
// rather than reflected so the test cannot silently skip a new field.
func applyColorOverride(c *config.Colors, name, value string) {
	switch name {
	case "status_fg":
		c.StatusFg = value
	case "status_bg":
		c.StatusBg = value
	case "fg":
		c.Fg = value
	case "bg":
		c.Bg = value
	case "selection":
		c.Selection = value
	case "search":
		c.Search = value
	case "cursor":
		c.Cursor = value
	case "palette.black":
		c.Palette.Black = value
	case "palette.red":
		c.Palette.Red = value
	case "palette.green":
		c.Palette.Green = value
	case "palette.yellow":
		c.Palette.Yellow = value
	case "palette.blue":
		c.Palette.Blue = value
	case "palette.magenta":
		c.Palette.Magenta = value
	case "palette.cyan":
		c.Palette.Cyan = value
	case "palette.white":
		c.Palette.White = value
	case "palette.bright_black":
		c.Palette.BrightBlack = value
	case "palette.bright_red":
		c.Palette.BrightRed = value
	case "palette.bright_green":
		c.Palette.BrightGreen = value
	case "palette.bright_yellow":
		c.Palette.BrightYellow = value
	case "palette.bright_blue":
		c.Palette.BrightBlue = value
	case "palette.bright_magenta":
		c.Palette.BrightMagenta = value
	case "palette.bright_cyan":
		c.Palette.BrightCyan = value
	case "palette.bright_white":
		c.Palette.BrightWhite = value
	default:
		panic("unhandled color field " + name)
	}
}

// applyHostColors must be a silent no-op when the user configured no colors:
// the default launch must never touch host console state.
func TestApplyHostColorsNoOpWhenUnconfigured(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	if err := a.applyConfig(config.Default()); err != nil {
		t.Fatal(err)
	}
	palette := [16]emulator.Color{}
	for i := range palette {
		palette[i] = emulator.Color{Default: true}
	}
	if err := a.applyHostColors(palette, nil, nil); err != nil {
		t.Fatalf("unconfigured palette should be a no-op, got %v", err)
	}
}

// A configured default foreground/background becomes what "terminal default"
// means: it overrides whatever the host console reported, because
// applyConfig installed it into the host buffer's own attributes.
func TestConfiguredFgBgOverridesHostTheme(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	a.themeFg = emulator.Color{R: 1, G: 2, B: 3}
	a.themeBg = emulator.Color{R: 4, G: 5, B: 6}
	a.haveTheme = true

	cfg := withColors(config.Colors{Fg: "#aaaaaa", Bg: "#bbbbbb"})
	if err := a.applyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	fg, bg, have := a.themeColors()
	if !have || fg != rgb("#aaaaaa") || bg != rgb("#bbbbbb") {
		t.Errorf("themeColors = fg %+v bg %+v have=%v", fg, bg, have)
	}
}

// With no configured fg/bg the host theme must survive untouched.
func TestThemeColorsFallBackToHost(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	a.themeFg = emulator.Color{R: 1, G: 2, B: 3}
	a.themeBg = emulator.Color{R: 4, G: 5, B: 6}
	a.haveTheme = true
	if err := a.applyConfig(config.Default()); err != nil {
		t.Fatal(err)
	}
	fg, bg, have := a.themeColors()
	if !have || fg != a.themeFg || bg != a.themeBg {
		t.Errorf("themeColors = fg %+v bg %+v have=%v", fg, bg, have)
	}
}

// A configured fg/bg counts as a known theme even when the host console could
// not be queried (headless hosts, or the query failing): the renderer needs
// real colors to invert the cursor block against.
func TestConfiguredFgProvidesThemeWhenHostHasNone(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	a.haveTheme = false
	cfg := withColors(config.Colors{Fg: "#aaaaaa"})
	if err := a.applyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	fg, _, have := a.themeColors()
	if !have || fg != rgb("#aaaaaa") {
		t.Errorf("themeColors = fg %+v have=%v, want the configured fg", fg, have)
	}
}

// Status line colors keep working alongside the new keys: an unset status color
// still falls back to the built-in defaults, not to the host theme.
func TestStatusColorsKeepTheirDefaults(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	cfg := withColors(config.Colors{Fg: "#aaaaaa"})
	if err := a.applyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	fg, bg := a.statusStyle()
	if fg != defaultStatusFg || bg != defaultStatusBg {
		t.Errorf("statusStyle = %+v/%+v, want the built-in defaults", fg, bg)
	}
}

// An unstyled highlight must be the reverse attribute, unchanged from before
// the palette feature existed.
func TestPaintHighlightUnstyledUsesReverse(t *testing.T) {
	cell := emulator.Cell{Content: "a"}
	paintHighlight(&cell, rgb("#264f78"), false)
	if !cell.Reverse {
		t.Error("unstyled highlight did not set Reverse")
	}
	if cell.Bg != (emulator.Color{}) {
		t.Errorf("unstyled highlight painted a background: %+v", cell.Bg)
	}
}

// A styled highlight paints its background and must clear Reverse, otherwise
// the host inverts the whole cell and the configured color is lost on any
// cell that was already highlighted.
func TestPaintHighlightStyledPaintsBackground(t *testing.T) {
	cell := emulator.Cell{Content: "a", Reverse: true}
	paintHighlight(&cell, rgb("#264f78"), true)
	if cell.Reverse {
		t.Error("styled highlight left Reverse set")
	}
	if cell.Bg != rgb("#264f78") {
		t.Errorf("background = %+v, want %+v", cell.Bg, rgb("#264f78"))
	}
}
