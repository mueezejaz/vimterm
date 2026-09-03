package app

import (
	"testing"

	"vimterm/internal/config"
	"vimterm/internal/emulator"
	"vimterm/internal/mode"
)

func TestMergeAllowed(t *testing.T) {
	def := emulator.Color{Default: true}
	styled := emulator.Cell{Content: "x", Width: 1, Fg: def, Bg: emulator.Color{R: 40, G: 40, B: 90}}

	plain := make([]emulator.Cell, 4)
	for i := range plain {
		plain[i] = emulator.Cell{Content: "x", Width: 1, Fg: def, Bg: def}
	}
	withBar := make([]emulator.Cell, 4)
	withBar[1] = styled

	cases := []struct {
		name string
		row  []emulator.Cell
		mode string
		want bool
	}{
		{"auto plain row", plain, "auto", false},
		{"auto statusline-like row", withBar, "auto", true},
		{"always plain row", plain, "always", true},
		{"never statusline-like row", withBar, "never", false},
	}
	for _, c := range cases {
		if got := mergeAllowed(c.row, c.mode); got != c.want {
			t.Errorf("%s: mergeAllowed = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestOverlayStatusMessage(t *testing.T) {
	def := emulator.Color{Default: true}
	row := make([]emulator.Cell, 40)
	for i := range row {
		row[i] = emulator.Cell{Content: "x", Width: 1, Fg: def, Bg: def}
	}
	fg := emulator.Color{R: 235, G: 235, B: 255}
	bg := emulator.Color{R: 40, G: 40, B: 90}

	overlayStatusMessage(row, mode.ModeNormal, "3 lines yanked", fg, bg)

	left := " NORMAL 3 lines yanked "
	for i, r := range left {
		c := row[i]
		if c.Content != string(r) {
			t.Fatalf("cell %d = %q, want %q", i, c.Content, string(r))
		}
		if c.Fg != fg || c.Bg != bg || !c.Bold {
			t.Fatalf("cell %d missing status styling: %+v", i, c)
		}
	}
	if row[len(left)].Content != "x" {
		t.Fatalf("cell after the overlay must keep the app's status line content")
	}
}

func TestOverlayStatusMessageTruncates(t *testing.T) {
	row := make([]emulator.Cell, 5)
	fg := emulator.Color{R: 235, G: 235, B: 255}
	bg := emulator.Color{R: 40, G: 40, B: 90}

	overlayStatusMessage(row, mode.ModeNormal, "a very long message", fg, bg)

	for i := range row {
		if row[i].Content == "" {
			t.Fatalf("all cells must be filled, cell %d empty", i)
		}
	}
}

func TestChildRows(t *testing.T) {
	shellApp := &App{cfg: config.Default()}
	altAuto := &App{altScreen: true, cfg: cfgWithMerge("auto")}
	altAlways := &App{altScreen: true, cfg: cfgWithMerge("always")}
	altNever := &App{altScreen: true, cfg: cfgWithMerge("never")}
	cases := []struct {
		name     string
		a        *App
		hostRows int
		want     int
	}{
		{"shell keeps one row for the status bar", shellApp, 30, 29},
		{"alt screen with auto gets full height", altAuto, 30, 30},
		{"alt screen with always gets full height", altAlways, 30, 30},
		{"alt screen with never keeps the status bar", altNever, 30, 29},
		{"tiny host keeps at least one row", altAuto, 1, 1},
	}
	for _, c := range cases {
		if got := c.a.childRows(c.hostRows); got != c.want {
			t.Errorf("%s: childRows(%d) = %d, want %d", c.name, c.hostRows, got, c.want)
		}
	}
}

func cfgWithMerge(merge string) *config.Config {
	cfg := config.Default()
	cfg.General.StatusMerge = merge
	return cfg
}
