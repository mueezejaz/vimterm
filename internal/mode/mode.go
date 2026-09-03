// Package mode implements the Vim-style mode state machine.
package mode

// Mode identifies the active input mode.
type Mode int

const (
	// ModeNormal: navigation and control; keys are interpreted through the
	// keybinding engine and never reach the shell.
	ModeNormal Mode = iota
	// ModeInsert: keys pass through to the shell verbatim.
	ModeInsert
	// ModeVisual: character-wise selection over the buffer; movement keys
	// extend the selection.
	ModeVisual
	// ModeVisualLine: line-wise selection; movement keys extend the
	// selection to whole lines.
	ModeVisualLine
)

// String returns the status-line label for the mode.
func (m Mode) String() string {
	switch m {
	case ModeInsert:
		return "INSERT"
	case ModeVisual:
		return "VISUAL"
	case ModeVisualLine:
		return "VISUAL LINE"
	default:
		return "NORMAL"
	}
}

// IsVisual reports whether the mode is one of the selection modes.
func (m Mode) IsVisual() bool {
	return m == ModeVisual || m == ModeVisualLine
}

// Manager tracks the current mode.
type Manager struct {
	current Mode
}

// NewManager returns a Manager starting in normal mode.
func NewManager() *Manager {
	return &Manager{current: ModeNormal}
}

// Current returns the active mode.
func (m *Manager) Current() Mode {
	return m.current
}

// Is reports whether the active mode is the given one.
func (m *Manager) Is(mode Mode) bool {
	return m.current == mode
}

// Enter switches to the given mode.
func (m *Manager) Enter(mode Mode) {
	m.current = mode
}
