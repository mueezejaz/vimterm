// Package macro implements Vim-style macro recording: q<register> records
// key sequences, @<register> replays them.
package macro

import "vimterm/internal/keybind"

// Outcome of feeding a key to the recorder.
type Outcome int

const (
	// OutcomeIgnored: nothing happened (not awaiting a register, not
	// recording).
	OutcomeIgnored Outcome = iota
	// OutcomePending: awaiting the register key.
	OutcomePending
	// OutcomeStarted: recording began for the given register.
	OutcomeStarted
	// OutcomeRecorded: the key was appended to the current recording.
	OutcomeRecorded
	// OutcomeReplayed: the register was found and queued for replay.
	OutcomeReplayed
	// OutcomeNoRegister: the requested register is empty or invalid.
	OutcomeNoRegister
)

// Recorder tracks register contents and the record/pending state. It is not
// safe for concurrent use; the app owns it from the main loop.
type Recorder struct {
	regs     map[rune][]keybind.Key
	pending  bool // awaiting the register key after q or @
	play     bool // pending is for playback (@) rather than recording (q)
	rec      bool // currently recording
	cur      rune
	buf      []keybind.Key
	replayed []keybind.Key
	replayIn bool // set while the app replays a register's keys
}

// New creates an empty recorder.
func New() *Recorder {
	return &Recorder{regs: make(map[rune][]keybind.Key)}
}

// StartPending arms the recorder for a register key: q starts recording,
// @ queues playback. If a recording is active it is stopped first.
func (r *Recorder) StartPending(play bool) {
	if r.rec {
		r.stop()
	}
	r.pending = true
	r.play = play
}

// CancelPending abandons an armed register.
func (r *Recorder) CancelPending() {
	r.pending = false
	r.play = false
}

// IsRecording reports whether recording is active.
func (r *Recorder) IsRecording() bool {
	return r.rec
}

// IsPending reports whether a register key is awaited.
func (r *Recorder) IsPending() bool {
	return r.pending
}

// Feed handles one key from the app's main loop. During recording every key
// is buffered; when a register is pending, the next letter key selects it.
func (r *Recorder) Feed(k keybind.Key) Outcome {
	if r.pending {
		reg, ok := registerRune(k)
		if !ok {
			r.pending = false
			return OutcomeIgnored
		}
		r.pending = false
		if r.play {
			seq, ok := r.regs[reg]
			if !ok {
				return OutcomeNoRegister
			}
			r.replayed = seq
			return OutcomeReplayed
		}
		r.cur = reg
		r.buf = nil
		r.rec = true
		return OutcomeStarted
	}
	if r.rec {
		r.buf = append(r.buf, k)
		return OutcomeRecorded
	}
	return OutcomeIgnored
}

// ReplayedSeq returns the sequence queued by the most recent OutcomeReplayed
// feed. The app replays it while Replaying reports true.
func (r *Recorder) ReplayedSeq() []keybind.Key {
	return r.replayed
}

// Replaying reports whether the app is currently replaying a register.
func (r *Recorder) Replaying() bool {
	return r.replayIn
}

// SetReplaying toggles the replay guard around a playback loop.
func (r *Recorder) SetReplaying(b bool) {
	r.replayIn = b
}

// CurrentReg returns the register being recorded, or 0.
func (r *Recorder) CurrentReg() rune {
	return r.cur
}

// StopTruncate ends recording, dropping the terminating key that was
// appended to the buffer (the q that stopped the recording).
func (r *Recorder) StopTruncate() {
	if !r.rec {
		return
	}
	if len(r.buf) > 0 {
		r.buf = r.buf[:len(r.buf)-1]
	}
	r.stop()
}

// Stop ends recording and stores the buffer in the current register.
func (r *Recorder) Stop() {
	r.stop()
}

func (r *Recorder) stop() {
	if !r.rec {
		return
	}
	r.rec = false
	if r.cur != 0 {
		r.regs[r.cur] = append([]keybind.Key(nil), r.buf...)
	}
	r.buf = nil
	r.cur = 0
}

// Has reports whether a register has a recorded sequence.
func (r *Recorder) Has(reg rune) bool {
	_, ok := r.regs[reg]
	return ok
}

// registerRune extracts the register name from a key: a letter with no
// modifiers.
func registerRune(k keybind.Key) (rune, bool) {
	if k.Code != keybind.CodeRune || k.Mods != 0 {
		return 0, false
	}
	if k.Rune >= 'a' && k.Rune <= 'z' {
		return k.Rune, true
	}
	return 0, false
}
