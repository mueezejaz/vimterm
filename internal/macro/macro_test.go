package macro

import (
	"testing"

	"vimterm/internal/keybind"
)

func k(r rune) keybind.Key { return keybind.NewRune(r, 0) }

func TestRecordStopAndReplay(t *testing.T) {
	r := New()
	r.StartPending(false)
	if got := r.Feed(k('a')); got != OutcomeStarted {
		t.Fatalf("Feed(a) = %v, want Started", got)
	}
	if !r.IsRecording() || r.CurrentReg() != 'a' {
		t.Fatalf("not recording register a")
	}
	for _, want := range []Outcome{OutcomeRecorded, OutcomeRecorded, OutcomeRecorded} {
		if got := r.Feed(k('i')); got != want {
			t.Fatalf("Feed during recording = %v, want %v", got, want)
		}
	}
	r.Stop()
	if r.IsRecording() {
		t.Fatal("still recording after Stop")
	}
	if !r.Has('a') {
		t.Fatal("register a not stored")
	}

	r.StartPending(true)
	if got := r.Feed(k('a')); got != OutcomeReplayed {
		t.Fatalf("play Feed = %v, want Replayed", got)
	}
	seq := r.ReplayedSeq()
	if len(seq) != 3 {
		t.Fatalf("replayed seq length = %d, want 3", len(seq))
	}
	if !seq[0].Equal(k('i')) {
		t.Errorf("first replayed key = %+v, want 'i'", seq[0])
	}
}

func TestStopTruncateDropsTerminatingKey(t *testing.T) {
	r := New()
	r.StartPending(false)
	r.Feed(k('a'))
	r.Feed(k('i'))
	r.Feed(k('x'))
	r.Feed(k('q')) // the terminating q is appended before the action runs
	r.StopTruncate()
	r.StartPending(true)
	if got := r.Feed(k('a')); got != OutcomeReplayed {
		t.Fatalf("play Feed = %v, want Replayed", got)
	}
	seq := r.ReplayedSeq()
	if len(seq) != 2 {
		t.Fatalf("stored seq length = %d, want 2 (i, x)", len(seq))
	}
	if !seq[0].Equal(k('i')) || !seq[1].Equal(k('x')) {
		t.Fatalf("stored seq = %+v, want [i x]", seq)
	}
}

func TestEmptyRegister(t *testing.T) {
	r := New()
	r.StartPending(true)
	if got := r.Feed(k('z')); got != OutcomeNoRegister {
		t.Fatalf("Feed(z) = %v, want NoRegister", got)
	}
}

func TestRegisterMustBeLowercaseLetter(t *testing.T) {
	r := New()
	r.StartPending(false)
	if got := r.Feed(k('X')); got != OutcomeIgnored {
		t.Fatalf("Feed(X) = %v, want Ignored", got)
	}
	if r.IsPending() {
		t.Fatal("pending not cleared after invalid register")
	}
}

func TestStartPendingStopsRecording(t *testing.T) {
	r := New()
	r.StartPending(false)
	r.Feed(k('a'))
	r.Feed(k('i'))
	r.StartPending(true) // @ while recording stops it
	if r.IsRecording() {
		t.Fatal("recording continued after StartPending(play)")
	}
	if !r.IsPending() {
		t.Fatal("play not pending")
	}
}

func TestReplayingGuard(t *testing.T) {
	r := New()
	r.SetReplaying(true)
	if !r.Replaying() {
		t.Fatal("Replaying() = false after SetReplaying(true)")
	}
	r.SetReplaying(false)
	if r.Replaying() {
		t.Fatal("Replaying() = true after SetReplaying(false)")
	}
}

func TestNestedPlayNotStarted(t *testing.T) {
	r := New()
	r.StartPending(false)
	r.Feed(k('a'))
	r.Feed(k('i'))
	r.StartPending(true)
	r.Feed(k('a'))
	if r.ReplayedSeq() == nil {
		t.Fatal("no replay queued")
	}
}
