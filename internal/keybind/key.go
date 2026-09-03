package keybind

// Mods is a bitmask of keyboard modifiers.
type Mods uint16

const (
	ModNone  Mods = 0
	ModShift Mods = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
)

// Code identifies a non-printable key.
type Code uint16

const (
	CodeRune Code = iota
	CodeEnter
	CodeBackspace
	CodeTab
	CodeEsc
	CodeSpace
	CodeLeft
	CodeRight
	CodeUp
	CodeDown
	CodeHome
	CodeEnd
	CodePageUp
	CodePageDown
	CodeInsert
	CodeDelete

	CodeF1
	CodeF2
	CodeF3
	CodeF4
	CodeF5
	CodeF6
	CodeF7
	CodeF8
	CodeF9
	CodeF10
	CodeF11
	CodeF12
	CodeF13
	CodeF14
	CodeF15
	CodeF16
	CodeF17
	CodeF18
	CodeF19
	CodeF20
	CodeF21
	CodeF22
	CodeF23
	CodeF24
)

// Key represents a single key press: either a printable rune (CodeRune) or a
// special key code, plus modifiers.
type Key struct {
	Code Code
	Rune rune
	Mods Mods
}

// NewRune builds a Key from a printable rune.
func NewRune(r rune, mods Mods) Key {
	return Key{Code: CodeRune, Rune: r, Mods: mods}
}

// NewCode builds a Key from a special key code.
func NewCode(c Code, mods Mods) Key {
	return Key{Code: c, Mods: mods}
}

// Equal reports whether two keys are the same.
func (k Key) Equal(o Key) bool {
	return k.Code == o.Code && k.Rune == o.Rune && k.Mods == o.Mods
}
