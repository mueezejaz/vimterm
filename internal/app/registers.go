package app

import (
	"strconv"
	"strings"
)

// registers implements Vim-style text registers for yank/delete/paste. The
// unnamed register ("") receives every yank and delete; "0 the last yank;
// "1-"9 rotate on deletes (oldest falls off at "9); "a-"z hold named text,
// with the uppercase form appending. The clipboard bridges in as "+" (and
// "*", its synonym on Windows) at paste time and yank time in the App layer,
// not here: this type never touches the Win32 clipboard.
//
// Maps are created lazily so a zero value works — the test harness builds
// App structs directly without a constructor.
type registers struct {
	hasUnnamed bool
	unnamed    string
	named      map[rune]string
	numbered   [10]string // numbered[0] is "0 (last yank); [1..9] are "1-"9
}

// setNamed stores text in a named register: lowercase overwrites, uppercase
// appends. Unknown register runes are ignored (the caller validates).
func (r *registers) setNamed(reg rune, text string) {
	if reg >= 'A' && reg <= 'Z' {
		reg += 'a' - 'A'
		if r.named == nil {
			r.named = make(map[rune]string)
		}
		r.named[reg] += text
		return
	}
	if reg >= 'a' && reg <= 'z' {
		if r.named == nil {
			r.named = make(map[rune]string)
		}
		r.named[reg] = text
	}
}

// setYank records a yank: unnamed, "0, and the explicit target register.
func (r *registers) setYank(text string, reg rune) {
	r.hasUnnamed = true
	r.unnamed = text
	r.numbered[0] = text
	if reg != 0 {
		r.setNamed(reg, text)
	}
}

// setDelete records a delete: unnamed, a shift of the "1-"9 rotation, and
// the explicit target register. Deletes never touch "0.
func (r *registers) setDelete(text string, reg rune) {
	r.hasUnnamed = true
	r.unnamed = text
	for i := 9; i > 1; i-- {
		r.numbered[i] = r.numbered[i-1]
	}
	r.numbered[1] = text
	if reg != 0 {
		r.setNamed(reg, text)
	}
}

// get returns a register's content. reg 0 is the unnamed register; '0 the
// last yank; '1-'9 the delete rotation. The clipboard registers ("+/"*) are
// resolved by the caller. ok is false for an unset or out-of-range register.
func (r *registers) get(reg rune) (string, bool) {
	switch {
	case reg == 0 || reg == '"':
		return r.unnamed, r.hasUnnamed
	case reg == '0':
		return r.numbered[0], true
	case reg >= '1' && reg <= '9':
		return r.numbered[reg-'0'], true
	case reg >= 'a' && reg <= 'z':
		return r.named[reg], r.named != nil && r.named[reg] != ""
	default:
		return "", false
	}
}

// registersSummary renders a compact one-line listing of the non-empty
// registers, for the :reg command's status message.
func (r *registers) registersSummary() string {
	truncate := func(s string) string {
		rs := []rune(s)
		if len(rs) > 20 {
			rs = append(rs[:20], '…')
		}
		return strings.ReplaceAll(strings.ReplaceAll(string(rs), "\r", ""), "\n", "\\n")
	}
	var sb strings.Builder
	add := func(name, text string) {
		if text == "" {
			return
		}
		if sb.Len() > 0 {
			sb.WriteString("  ")
		}
		sb.WriteString(name)
		sb.WriteByte(' ')
		sb.WriteString(truncate(text))
	}
	if r.hasUnnamed {
		add("\"", r.unnamed)
	}
	add("0", r.numbered[0])
	for i := 1; i < len(r.numbered); i++ {
		add(strconv.Itoa(i), r.numbered[i])
	}
	for c := 'a'; c <= 'z'; c++ {
		if text, ok := r.get(c); ok {
			add(string(c), text)
		}
	}
	if sb.Len() == 0 {
		return "no registers"
	}
	return sb.String()
}
