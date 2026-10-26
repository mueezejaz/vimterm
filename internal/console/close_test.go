package console

// Regression test for the done-channel close protocol: the input loop's and
// resize poller's panic handlers used to close done directly, so a panic
// followed the usual Close (which also closes done) double-closed the channel
// and panicked mid-teardown, leaving the host terminal in raw mode.

import "testing"

func TestCloseDoneIdempotent(t *testing.T) {
	c := &Console{done: make(chan struct{}), errc: make(chan error, 1)}
	// The panic path fires first: the goroutine closes done on its way out.
	c.closeDone()
	// Close must tolerate the channel already being closed (it reports
	// errors from the restore calls on a zero-value Console; only a panic
	// from double-close is a failure here).
	_ = c.Close()
	// A late panic handler must not panic either.
	c.closeDone()
	select {
	case <-c.done:
	default:
		t.Fatal("done was not closed")
	}
}
