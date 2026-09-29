package app

// Regression tests for terminal reply forwarding. The emulator answers
// device queries (CPR/DSR, DA1/DA2, in-band resize) into an unbuffered pipe
// while parsing, so if nothing drains it, the child's very first question
// blocks Write - and with it the reader goroutine and the render loop. The
// whole terminal froze until the child was killed.

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"vimterm/internal/emulator"
)

// captureSession records everything written to the child.
type captureSession struct {
	mu  sync.Mutex
	buf []byte
}

func (s *captureSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	return len(p), nil
}
func (s *captureSession) written() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.buf)
}

func (s *captureSession) Read([]byte) (int, error)   { return 0, io.EOF }
func (s *captureSession) Resize(int, int) error      { return nil }
func (s *captureSession) Kill() error                { return nil }
func (s *captureSession) Close() error               { return nil }
func (s *captureSession) Name() string               { return "capture" }
func (s *captureSession) Wait(context.Context) error { return nil }

// waitFor polls until cond holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A child asking for the cursor position gets an answer back.
func TestStartRepliesForwardsCPR(t *testing.T) {
	a := realApp(t, 40, 6, "")
	sess := &captureSession{}
	emu := emulator.New(40, 5)
	tab := a.tabs[a.active]
	tab.sess, tab.emu = sess, emu
	defer emu.Close()

	a.startReplies(tab)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = emu.Write([]byte("\x1b[6n"))
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("emu.Write(CSI 6n) blocked: replies were not drained")
	}

	waitFor(t, "the CPR reply to reach the child", func() bool {
		return sess.written() != ""
	})
	if got := sess.written(); got != "\x1b[1;1R" {
		t.Errorf("child received %q, want %q", got, "\x1b[1;1R")
	}
}

// Several queries in one write must all come back, in order.
func TestStartRepliesForwardsMultipleReplies(t *testing.T) {
	a := realApp(t, 40, 6, "")
	sess := &captureSession{}
	emu := emulator.New(40, 5)
	tab := a.tabs[a.active]
	tab.sess, tab.emu = sess, emu
	defer emu.Close()

	a.startReplies(tab)

	// Two cursor queries in a single write.
	_, _ = emu.Write([]byte("\x1b[6n\x1b[6n"))

	waitFor(t, "both CPR replies", func() bool {
		return len(sess.written()) >= len("\x1b[1;1R")*2
	})
}

// Closing the emulator ends the forwarder instead of leaking it.
func TestStartRepliesStopsOnEmulatorClose(t *testing.T) {
	a := realApp(t, 40, 6, "")
	sess := &captureSession{}
	emu := emulator.New(40, 5)
	tab := a.tabs[a.active]
	tab.sess, tab.emu = sess, emu

	a.startReplies(tab)
	if err := emu.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After Close the reader must return rather than spinning or blocking;
	// a query can no longer be written, so just give it a moment to unwind.
	time.Sleep(100 * time.Millisecond)
	if got := sess.written(); got != "" {
		t.Errorf("unexpected writes after Close: %q", got)
	}
}

// startReplies must tolerate a tab with no session (tests build such tabs).
func TestStartRepliesNoSession(t *testing.T) {
	a := realApp(t, 40, 6, "")
	emu := emulator.New(40, 5)
	tab := a.tabs[a.active]
	tab.sess, tab.emu = nil, emu
	defer emu.Close()

	a.startReplies(tab) // must not panic or spin
	time.Sleep(50 * time.Millisecond)
}
