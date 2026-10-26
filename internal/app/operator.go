package app

// Operator-pending mode: d/c/y wait for a motion or text object, then apply
// the operation over the covered range. The motion keymap is built in code
// (operatorKeymap) rather than from user config — the same motion tokens as
// normal mode dispatch through runOperator instead of moving the cursor.
//
// Ranges are computed in rune space per line and converted to cell segments
// at deletion time, so wide characters are never split. Whole-line ranges
// blank the line's cells (the terminal buffer cannot remove lines the child
// owns); deletions on the shell's own line are mirrored into its line editor
// by propagateDelete, exactly as dw has always done.

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"vimterm/internal/keybind"
	"vimterm/internal/mode"
	"vimterm/internal/selection"
)

// Text-object actions, meaningful only in the operator keymap. The pair
// character for opTextI/opTextA is the rune of the last matched key.
const (
	opTextIW     keybind.Action = "op_text_iw"
	opTextAW     keybind.Action = "op_text_aw"
	opTextIWWORD keybind.Action = "op_text_iw_word"
	opTextAWWORD keybind.Action = "op_text_aw_word"
	opTextI      keybind.Action = "op_text_i"
	opTextA      keybind.Action = "op_text_a"
)

// opState is the armed operator: which operation, the count typed before it,
// the motion count typed after it, the keys that armed it (for "." repeat),
// and the composed count.
type opState struct {
	kind   keybind.Action
	count  int
	motion int
	total  int
	keys   []keybind.Key
}

// opRange is the buffer span an operator covers, in rune indices per line.
// A same-line range with fromRune > toRune is empty.
type opRange struct {
	fromLine, fromRune int
	toLine, toRune     int
	lineWise           bool
}

// operatorKeymap builds the operator-pending keymap: the motion tokens of
// normal mode plus the text objects. Built in code, not from user config.
func operatorKeymap(leader keybind.Key) (*keybind.Keymap, error) {
	tokens := map[string]keybind.Action{
		"h":      keybind.ActionMoveLeft,
		"j":      keybind.ActionMoveDown,
		"k":      keybind.ActionMoveUp,
		"l":      keybind.ActionMoveRight,
		"w":      keybind.ActionMoveWord,
		"W":      keybind.ActionMoveWORD,
		"b":      keybind.ActionMoveWordBack,
		"B":      keybind.ActionMoveWORDBack,
		"e":      keybind.ActionMoveWordEnd,
		"E":      keybind.ActionMoveWORDEnd,
		"$":      keybind.ActionMoveLineEnd,
		"^":      keybind.ActionMoveLineBeg,
		"0":      keybind.ActionMoveLineStart,
		"gg":     keybind.ActionGotoTop,
		"G":      keybind.ActionGotoBottom,
		"ctrl+u": keybind.ActionScrollUp,
		"ctrl+d": keybind.ActionScrollDown,
		"f":      keybind.ActionFindChar,
		"F":      keybind.ActionFindCharBack,
		"t":      keybind.ActionFindUntil,
		"T":      keybind.ActionFindUntilBack,
		";":      keybind.ActionFindNext,
		",":      keybind.ActionFindPrev,
		"d":      keybind.ActionOpDelete,
		"c":      keybind.ActionOpChange,
		"y":      keybind.ActionOpYank,
		"esc":    keybind.ActionEnterNormal,
		"iw":     opTextIW,
		"aw":     opTextAW,
		"iW":     opTextIWWORD,
		"aW":     opTextAWWORD,
	}
	for _, q := range []rune{'"', '\'', '`', '(', ')', '[', ']', '{', '}', '<', '>'} {
		tokens["i"+string(q)] = opTextI
		tokens["a"+string(q)] = opTextA
	}
	km := keybind.NewKeymap()
	for token, act := range tokens {
		seq, err := keybind.ParseSequence(token, leader)
		if err != nil {
			return nil, fmt.Errorf("operator binding %q: %w", token, err)
		}
		km.Bind(seq, act)
	}
	return km, nil
}

// beginOperator arms the operator with the count typed before it and enters
// operator-pending mode.
func (a *App) beginOperator(kind keybind.Action) {
	a.sel.Cancel()
	a.op = opState{kind: kind, count: a.takeCount(), keys: a.engine.LastSeq()}
	a.mods.Enter(mode.ModeOperator)
	a.dirty.Store(true)
}

// cancelOperator disarms a pending operator (esc, dead-end key, tab switch).
func (a *App) cancelOperator() {
	a.op = opState{}
	if a.mods.Is(mode.ModeOperator) {
		a.mods.Enter(mode.ModeNormal)
	}
}

// opTotal composes the count typed before the operator with the motion's own
// count (2d3w operates over 6 words) and remembers it for "." replay.
func (a *App) opTotal() int {
	total := a.op.motion
	if a.op.count > 0 {
		total = a.op.count * a.op.motion
	}
	if total > maxCount {
		total = maxCount
	}
	a.op.total = total
	return total
}

// takeMotionCount consumes the count digits typed since the operator was
// armed (the d2 in d2w). The pre-operator count goes through countAware into
// op.count at arm time; this is the one typed while waiting for the motion.
func (a *App) takeMotionCount() {
	a.op.motion = a.count
	a.count = 0
	if a.op.motion < 1 {
		a.op.motion = 1
	}
}

// runOperator dispatches one Matched key while an operator is pending: the
// operator keys themselves make the operation linewise (dd, d2d), find
// motions arm the pending-target state, everything else evaluates a range
// and finishes the operation.
func (a *App) runOperator(act keybind.Action) {
	switch act {
	case keybind.ActionEnterNormal:
		a.cancelOperator()
		a.dirty.Store(true)
		return
	case keybind.ActionOpDelete, keybind.ActionOpChange, keybind.ActionOpYank:
		a.takeMotionCount()
		a.finishOperator(a.linewiseRange(a.opTotal()))
		return
	case keybind.ActionFindChar:
		a.find.begin(1, false)
		return
	case keybind.ActionFindCharBack:
		a.find.begin(-1, false)
		return
	case keybind.ActionFindUntil:
		a.find.begin(1, true)
		return
	case keybind.ActionFindUntilBack:
		a.find.begin(-1, true)
		return
	case keybind.ActionFindNext:
		a.opFindRepeat(1)
		return
	case keybind.ActionFindPrev:
		a.opFindRepeat(-1)
		return
	case keybind.ActionMoveWord:
		a.takeMotionCount()
		a.finishWordOp(1, wordKindWord, a.opTotal())
		return
	case keybind.ActionMoveWordBack:
		a.takeMotionCount()
		a.finishWordOp(-1, wordKindWord, a.opTotal())
		return
	case keybind.ActionMoveWORD:
		a.takeMotionCount()
		a.finishWordOp(1, wordKindWORD, a.opTotal())
		return
	case keybind.ActionMoveWORDBack:
		a.takeMotionCount()
		a.finishWordOp(-1, wordKindWORD, a.opTotal())
		return
	case opTextIW, opTextAW, opTextIWWORD, opTextAWWORD, opTextI, opTextA:
		rng, ok := a.evalTextObject(act)
		if !ok {
			a.opFail("no text object")
			return
		}
		a.finishOperator(rng)
		return
	}
	a.takeMotionCount()
	rng, ok := a.evalOpMotion(act, a.opTotal())
	if !ok {
		a.opFail("no motion")
		return
	}
	a.finishOperator(rng)
}

// opFail cancels the operator with a message.
func (a *App) opFail(msg string) {
	a.cancelOperator()
	a.setStatusMsg(msg)
	a.dirty.Store(true)
}

// finishOperator applies the armed operation to the range and leaves
// operator-pending mode (change hands off to insert mode via enterInsert).
func (a *App) finishOperator(rng opRange) {
	switch a.op.kind {
	case keybind.ActionOpYank:
		a.yankOpRange(rng)
	case keybind.ActionOpChange:
		a.deleteOpRange(rng)
		a.enterInsert()
	default:
		a.deleteOpRange(rng)
	}
	a.noteOpRepeat()
	a.cancelOperator()
}

// finishWordOp routes w/b through the word-delete scan for every operation
// kind, keeping dw/db behavior identical to the pre-operator implementation.
func (a *App) finishWordOp(dir, kind, n int) {
	if a.op.kind == keybind.ActionOpYank {
		_, text, _ := a.scanWordDelete(dir, kind, n, false)
		a.storeYanked(text)
		a.dirty.Store(true)
		a.noteOpRepeat()
		a.cancelOperator()
		return
	}
	a.deleteWordCount(dir, kind, n)
	if a.op.kind == keybind.ActionOpChange {
		a.enterInsert()
	}
	a.noteOpRepeat()
	a.cancelOperator()
}

// noteOpRepeat records the whole operator sequence (operator keys plus the
// motion's) as the repeat unit, with the composed count.
func (a *App) noteOpRepeat() {
	if a.repeating {
		return
	}
	seq := append(append([]keybind.Key(nil), a.op.keys...), a.engine.LastSeq()...)
	if len(seq) == 0 {
		return
	}
	a.tracker.noteOperator(seq, a.op.total)
}

// storeYanked puts text in the registers and the clipboard (yank has always
// mirrored to the clipboard; deletes do not). clipWrite is nil in bare test
// harnesses.
func (a *App) storeYanked(text string) {
	a.regs.setYank(text, a.opReg)
	a.opReg = 0
	if a.clipWrite != nil {
		if err := a.clipWrite(text); err != nil {
			a.setStatusMsg("clipboard: " + err.Error())
		}
	}
}

// storeDeleted puts deleted text in the registers without touching the
// clipboard, so a delete never clobbers a previous yank.
func (a *App) storeDeleted(text string) {
	a.regs.setDelete(text, a.opReg)
	a.opReg = 0
}

// linewiseRange covers n whole lines starting at the cursor.
func (a *App) linewiseRange(n int) opRange {
	a.syncCursor()
	bottom := a.emu.ScrollbackLen() + a.emu.Height() - 1
	to := a.cur.Line + n - 1
	if to > bottom {
		to = bottom
	}
	return opRange{fromLine: a.cur.Line, toLine: to, lineWise: true}
}

// evalOpMotion evaluates a motion in operator context and returns the
// covered rune range (toRune inclusive). Same-line ranges may be empty
// (fromRune > toRune), which the caller treats as a no-op rather than a
// failure so c$ at end of line still enters insert.
func (a *App) evalOpMotion(act keybind.Action, n int) (opRange, bool) {
	a.syncCursor()
	line := a.cur.Line
	row := rowOf(a.bufferLineCells(line))
	ri := row.runeAt(a.cur.Col)
	te := textEnd(row.runes)
	bottom := a.emu.ScrollbackLen() + a.emu.Height() - 1
	switch act {
	case keybind.ActionMoveLeft:
		if ri == 0 {
			return opRange{}, false
		}
		from := max(ri-n, 0)
		return opRange{fromLine: line, fromRune: from, toLine: line, toRune: ri - 1}, true
	case keybind.ActionMoveRight:
		limit := te
		if limit < ri {
			limit = ri // at or past the content end: go no further
		}
		return opRange{fromLine: line, fromRune: ri, toLine: line, toRune: min(ri+n-1, limit)}, true
	case keybind.ActionMoveLineEnd:
		// Inclusive of the last content character.
		return opRange{fromLine: line, fromRune: ri, toLine: line, toRune: te}, true
	case keybind.ActionMoveLineBeg:
		fn := firstNonSpace(row.runes)
		if ri <= fn {
			return opRange{}, false
		}
		return opRange{fromLine: line, fromRune: fn, toLine: line, toRune: ri - 1}, true
	case keybind.ActionMoveLineStart:
		if ri == 0 {
			return opRange{}, false
		}
		return opRange{fromLine: line, fromRune: 0, toLine: line, toRune: ri - 1}, true
	case keybind.ActionMoveUp:
		return opRange{fromLine: max(line-n, 0), toLine: line, lineWise: true}, true
	case keybind.ActionMoveDown:
		return opRange{fromLine: line, toLine: min(line+n, bottom), lineWise: true}, true
	case keybind.ActionGotoTop:
		return opRange{fromLine: 0, toLine: line, lineWise: true}, true
	case keybind.ActionGotoBottom:
		return opRange{fromLine: line, toLine: bottom, lineWise: true}, true
	case keybind.ActionMoveWordEnd, keybind.ActionMoveWORDEnd:
		kind := wordKindWord
		if act == keybind.ActionMoveWORDEnd {
			kind = wordKindWORD
		}
		el, er, ok := a.opWordEndRune(n, kind)
		if !ok {
			return opRange{}, false
		}
		// Inclusive: the character under the target word end is covered.
		return normRange(line, ri, el, er, false), true
	case keybind.ActionScrollUp, keybind.ActionScrollDown:
		step := a.emu.Height() / 2
		if step < 1 {
			step = 1
		}
		if act == keybind.ActionScrollUp {
			return opRange{fromLine: max(line-step, 0), toLine: line, lineWise: true}, true
		}
		return opRange{fromLine: line, toLine: min(line+step, bottom), lineWise: true}, true
	}
	return opRange{}, false
}

// opWordEndRune walks n word ends forward (crossing lines like the e
// motion) and returns the landing position in rune indices.
func (a *App) opWordEndRune(n, kind int) (line, runeIdx int, ok bool) {
	line = a.cur.Line
	col := rowOf(a.bufferLineCells(line)).runeAt(a.cur.Col)
	bottom := a.emu.ScrollbackLen() + a.emu.Height() - 1
	for i := 0; i < n; i++ {
		found := -1
		for {
			r := rowOf(a.bufferLineCells(line))
			col = wordEnd(r.runes, col, kind)
			if col >= 0 {
				found = col
				break
			}
			line++
			if line > bottom {
				return 0, 0, false
			}
			col = 0
		}
		if found < 0 {
			return 0, 0, false
		}
	}
	return line, col, true
}

// normRange orders a range so from <= to.
func normRange(fromLine, fromRune, toLine, toRune int, lineWise bool) opRange {
	if fromLine > toLine || (fromLine == toLine && fromRune > toRune) {
		fromLine, toLine = toLine, fromLine
		fromRune, toRune = toRune, fromRune
	}
	return opRange{fromLine: fromLine, fromRune: fromRune, toLine: toLine, toRune: toRune, lineWise: lineWise}
}

// opScanRange materializes a range into per-line cell deletion segments plus
// the text it covers. At most one segment per line is produced, all in that
// line's current (pre-deletion) coordinates. It also returns the cell column
// the cursor should take on the range's first line.
func (a *App) opScanRange(rng opRange) ([]delSeg, string, int) {
	var sb strings.Builder
	var segs []delSeg
	curCol := 0
	multi := rng.toLine > rng.fromLine
	for l := rng.fromLine; l <= rng.toLine; l++ {
		cells := a.bufferLineCells(l)
		if cells == nil {
			break
		}
		if l > rng.fromLine && multi {
			sb.WriteByte('\n')
		}
		row := rowOf(cells)
		te := textEnd(row.runes)
		var rs, re int
		switch {
		case rng.lineWise:
			rs, re = 0, te
		case l == rng.fromLine && l == rng.toLine:
			rs, re = rng.fromRune, rng.toRune
		case l == rng.fromLine:
			rs, re = rng.fromRune, te
		case l == rng.toLine:
			rs, re = 0, rng.toRune
		default:
			rs, re = 0, te
		}
		if l == rng.fromLine {
			curCol = row.colAt(max(rs, 0))
		}
		if rng.lineWise {
			// Whole-line operations blank the full row (padding included) so
			// dd on the shell's line clears it entirely; the yanked text
			// stays trimmed of the padding.
			if len(cells) > 0 {
				segs = append(segs, delSeg{l, 0, len(cells)})
			}
			if te >= 0 {
				sb.WriteString(string(row.runes[0 : te+1]))
			}
			continue
		}
		if te >= 0 {
			re = min(re, te)
		}
		re = min(re, len(row.runes)-1)
		if rs > re || re < 0 || rs < 0 {
			continue // nothing to remove on this line (padding or blank)
		}
		sb.WriteString(string(row.runes[rs : re+1]))
		segs = append(segs, delSeg{l, row.colAt(rs), row.colAt(re + 1)})
	}
	return segs, sb.String(), curCol
}

// deleteOpRange deletes the range's cells, mirrors the deletion into the
// shell's line editor where applicable, stores the text in the registers and
// parks the cursor at the range start.
func (a *App) deleteOpRange(rng opRange) {
	segs, text, curCol := a.opScanRange(rng)
	for _, s := range segs {
		a.emu.DeleteLineCells(s.line, s.from, s.to-s.from)
	}
	a.cur = selection.Pos{Line: rng.fromLine, Col: curCol}
	if rng.lineWise {
		a.cur.Col = 0
	}
	a.clampCursor()
	a.ensureCursorVisible()
	a.dirty.Store(true)
	a.propagateDelete(segs)
	if text != "" {
		a.storeDeleted(text)
	}
}

// yankOpRange copies the range's text into the registers and the clipboard.
func (a *App) yankOpRange(rng opRange) {
	_, text, _ := a.opScanRange(rng)
	a.storeYanked(text)
	if rng.lineWise {
		a.setStatusMsg(fmt.Sprintf("%d lines yanked", rng.toLine-rng.fromLine+1))
	} else {
		a.setStatusMsg(fmt.Sprintf("%d chars yanked", utf8.RuneCountInString(text)))
	}
	a.dirty.Store(true)
}

// opFindCol locates the count-th raw occurrence of ch from the cursor (the
// f semantics; the caller adjusts for t/T).
func (a *App) opFindCol(dir int, ch rune, n int, skipFirst bool) int {
	row := rowOf(a.bufferLineCells(a.cur.Line))
	return findOnLine(row.runes, row.runeAt(a.cur.Col), dir, false, ch, n, skipFirst)
}

// opFindTarget applies the armed operator over the f/F/t/T range named by
// the just-typed target character.
func (a *App) opFindTarget(dir int, until bool, ch rune) {
	a.find.run(dir, until, ch)
	a.takeMotionCount()
	col := a.opFindCol(dir, ch, a.opTotal(), false)
	if col < 0 {
		a.opFail(fmt.Sprintf("find: no %q %s", ch, dirName(dir)))
		return
	}
	to := col
	if until {
		to = col - dir
		if (dir > 0 && to < a.curRuneIdx()) || (dir < 0 && to > a.curRuneIdx()) {
			a.opFail("find: no motion")
			return
		}
	}
	a.finishOperator(normRange(a.cur.Line, a.curRuneIdx(), a.cur.Line, to, false))
}

// opFindRepeat applies the operator over a ;/, repeat of the last find.
func (a *App) opFindRepeat(flip int) {
	if !a.find.set {
		a.opFail("no previous find")
		return
	}
	a.takeMotionCount()
	dir := a.find.dir * flip
	skip := a.find.until && a.opTotal() == 1
	col := a.opFindCol(dir, a.find.ch, a.opTotal(), skip)
	if col < 0 {
		a.opFail(fmt.Sprintf("find: no %q %s", a.find.ch, dirName(dir)))
		return
	}
	to := col
	if a.find.until {
		to = col - dir
		if (dir > 0 && to < a.curRuneIdx()) || (dir < 0 && to > a.curRuneIdx()) {
			a.opFail("find: no motion")
			return
		}
	}
	a.finishOperator(normRange(a.cur.Line, a.curRuneIdx(), a.cur.Line, to, false))
}

// curRuneIdx returns the cursor column as a rune index on its line.
func (a *App) curRuneIdx() int {
	row := rowOf(a.bufferLineCells(a.cur.Line))
	return row.runeAt(a.cur.Col)
}

// evalTextObject resolves a text object action to a range. Word objects
// ignore counts; quoted and bracket pairs are same-line only.
func (a *App) evalTextObject(act keybind.Action) (opRange, bool) {
	switch act {
	case opTextIW:
		return a.opWordObject(wordKindWord, false)
	case opTextAW:
		return a.opWordObject(wordKindWord, true)
	case opTextIWWORD:
		return a.opWordObject(wordKindWORD, false)
	case opTextAWWORD:
		return a.opWordObject(wordKindWORD, true)
	case opTextI, opTextA:
		seq := a.engine.LastSeq()
		if len(seq) == 0 {
			return opRange{}, false
		}
		ch := seq[len(seq)-1].Rune
		if strings.ContainsRune("\"'`", ch) {
			return a.opQuoteObject(ch, act == opTextA)
		}
		return a.opBracketObject(ch, act == opTextA)
	}
	return opRange{}, false
}

// opWordObject implements iw/aw/iW/aW: the word (or whitespace/punctuation
// run) under the cursor, extended over trailing whitespace for the around
// form (or leading whitespace when there is none).
func (a *App) opWordObject(kind int, around bool) (opRange, bool) {
	a.syncCursor()
	row := rowOf(a.bufferLineCells(a.cur.Line))
	if len(row.runes) == 0 {
		return opRange{}, false
	}
	runes := row.runes
	ci := row.runeAt(a.cur.Col)
	isWS := func(r rune) bool { return r == ' ' || r == '\t' }
	class := func(r rune) bool { return wordChar(kind, r) }
	var s, e int
	switch {
	case class(runes[ci]):
		s, e = ci, ci
		for s > 0 && class(runes[s-1]) {
			s--
		}
		for e+1 < len(runes) && class(runes[e+1]) {
			e++
		}
	case isWS(runes[ci]):
		s, e = ci, ci
		for s > 0 && isWS(runes[s-1]) {
			s--
		}
		for e+1 < len(runes) && isWS(runes[e+1]) {
			e++
		}
	default: // punctuation run
		other := func(r rune) bool { return !class(r) && !isWS(r) }
		s, e = ci, ci
		for s > 0 && other(runes[s-1]) {
			s--
		}
		for e+1 < len(runes) && other(runes[e+1]) {
			e++
		}
	}
	if around {
		e2 := e
		for e2+1 < len(runes) && isWS(runes[e2+1]) {
			e2++
		}
		if e2 > e {
			e = e2
		} else {
			for s > 0 && isWS(runes[s-1]) {
				s--
			}
		}
	}
	return normRange(a.cur.Line, s, a.cur.Line, e, false), true
}

// opQuoteObject implements i"/a" (and ' and `): the quoted string containing
// the cursor, or the next one on the line when the cursor is outside any.
func (a *App) opQuoteObject(q rune, around bool) (opRange, bool) {
	a.syncCursor()
	row := rowOf(a.bufferLineCells(a.cur.Line))
	ci := row.runeAt(a.cur.Col)
	var idxs []int
	for i, r := range row.runes {
		if r == q {
			idxs = append(idxs, i)
		}
	}
	before := 0
	for _, x := range idxs {
		if x < ci {
			before++
		}
	}
	var open, close int
	if before%2 == 1 {
		// Inside a pair (this includes sitting on its closing quote).
		if before < 1 {
			return opRange{}, false
		}
		open, close = idxs[before-1], idxs[before]
	} else {
		// Outside: take the next pair on the line.
		if before+1 >= len(idxs) {
			return opRange{}, false
		}
		open, close = idxs[before], idxs[before+1]
	}
	if !around {
		if close-open < 2 {
			return opRange{}, false
		}
		open++
		close--
	}
	return normRange(a.cur.Line, open, a.cur.Line, close, false), true
}

// bracketPairs maps openers to their closers.
var bracketPairs = map[rune]rune{'(': ')', '[': ']', '{': '}', '<': '>'}

// opBracketObject implements i(/a( and friends: the innermost bracket pair
// containing the cursor on the current line. Cursor on a bracket counts as
// being inside its pair.
func (a *App) opBracketObject(ch rune, around bool) (opRange, bool) {
	o, c := rune(0), rune(0)
	if cl, ok := bracketPairs[ch]; ok {
		o, c = ch, cl
	} else {
		for op, cl := range bracketPairs {
			if cl == ch {
				o, c = op, cl
			}
		}
		if o == 0 {
			return opRange{}, false
		}
	}
	a.syncCursor()
	row := rowOf(a.bufferLineCells(a.cur.Line))
	runes := row.runes
	ci := row.runeAt(a.cur.Col)
	open := -1
	switch {
	case runes[ci] == o:
		open = ci
	case runes[ci] == c:
		// On a closer: its opener is the nearest unmatched one to the left.
		depth := 0
		for i := ci - 1; i >= 0; i-- {
			switch runes[i] {
			case c:
				depth++
			case o:
				if depth == 0 {
					open = i
				} else {
					depth--
				}
			}
			if open >= 0 {
				break
			}
		}
	default:
		depth := 0
		for i := ci; i >= 0; i-- {
			switch runes[i] {
			case c:
				depth++
			case o:
				if depth == 0 {
					open = i
				} else {
					depth--
				}
			}
			if open >= 0 {
				break
			}
		}
	}
	if open < 0 {
		return opRange{}, false
	}
	close := -1
	depth := 0
	for i := open + 1; i < len(runes); i++ {
		switch runes[i] {
		case o:
			depth++
		case c:
			if depth == 0 {
				close = i
			} else {
				depth--
			}
		}
		if close >= 0 {
			break
		}
	}
	if close < 0 {
		return opRange{}, false
	}
	if !around {
		if close-open < 2 {
			return opRange{}, false
		}
		open++
		close--
	}
	return normRange(a.cur.Line, open, a.cur.Line, close, false), true
}

// deleteVisual deletes the visual selection through the operator machinery:
// registers, per-line segments, shell propagation. Selection columns are
// cell columns, so they convert to rune indices per line first.
func (a *App) deleteVisual() {
	if !a.sel.Active {
		a.setStatusMsg("nothing selected")
		a.dirty.Store(true)
		return
	}
	start, end := a.sel.Start(), a.sel.End()
	sRow := rowOf(a.bufferLineCells(start.Line))
	eRow := rowOf(a.bufferLineCells(end.Line))
	rng := opRange{
		fromLine: start.Line, fromRune: sRow.runeAt(start.Col),
		toLine: end.Line, toRune: eRow.runeAt(end.Col),
		lineWise: a.sel.LineWise,
	}
	a.sel.Cancel()
	a.mods.Enter(mode.ModeNormal)
	a.deleteOpRange(rng)
}

// markNameKey validates the key following m/`/' as a mark name.
func markNameKey(k keybind.Key) (rune, bool) {
	if k.Code != keybind.CodeRune || k.Mods&^keybind.ModShift != 0 {
		return 0, false
	}
	if k.Rune >= 'a' && k.Rune <= 'z' {
		return k.Rune, true
	}
	return 0, false
}

// registerNameKey validates the key following " as a register name: letters,
// digits, and the special "+, "*, "- and "" registers.
func registerNameKey(k keybind.Key) (rune, bool) {
	if k.Code != keybind.CodeRune || k.Mods&^keybind.ModShift != 0 {
		return 0, false
	}
	r := k.Rune
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return r, true
	case r == '+' || r == '*' || r == '-' || r == '"':
		return r, true
	}
	return 0, false
}
