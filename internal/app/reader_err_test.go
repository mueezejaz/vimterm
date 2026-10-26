package app

// Regression tests for reader error bookkeeping across session generations:
// a superseded session's teardown error must not poison the tab (it used to
// resurface as a bogus "child read" failure when a later shell exited).

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"vimterm/internal/pty"
)

// errSession returns one read error after release is closed, then EOF.
type errSession struct {
	release chan struct{}
	once    sync.Once
	err     error
	written bool
}

func (s *errSession) Write(p []byte) (int, error) { return len(p), nil }
func (s *errSession) Read(p []byte) (int, error) {
	select {
	case <-s.release:
	default:
		return 0, nil
	}
	s.once.Do(func() {})
	if s.written {
		return 0, io.EOF
	}
	s.written = true
	return 0, s.err
}
func (s *errSession) Resize(int, int) error          { return nil }
func (s *errSession) Kill() error                    { return nil }
func (s *errSession) Close() error                   { return nil }
func (s *errSession) Name() string                   { return "err" }
func (s *errSession) Wait(ctx context.Context) error { return nil }

func waitClosed(t *testing.T, ch chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ch:
			return
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	t.Fatal("timed out waiting for channel")
}

// A current-generation reader records its read error and closes done.
func TestReaderStoresErrorCurrentGen(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	tt := a.tabs[a.active]
	release := make(chan struct{})
	tt.sess = &errSession{release: release, err: errors.New("boom")}
	a.startReader(tt)
	close(release)
	waitClosed(t, tt.done)
	if tt.err.Load() == nil {
		t.Fatal("current-gen reader did not store its read error")
	}
}

// A superseded reader (gen bumped while parked in Read) stores nothing and
// leaves done alone.
func TestReaderDropsErrorAfterGenBump(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	tt := a.tabs[a.active]
	release := make(chan struct{})
	tt.sess = &errSession{release: release, err: errors.New("stale")}
	a.startReader(tt)
	tt.gen.Add(1)
	close(release)
	time.Sleep(50 * time.Millisecond)
	if tt.err.Load() != nil {
		t.Fatal("superseded reader stored its error into the tab")
	}
	select {
	case <-tt.done:
		t.Fatal("superseded reader closed done")
	default:
	}
}

// restartShell clears any error recorded for the previous session, so a
// deliberate restart cannot make the next exit look like a read failure.
func TestRestartShellResetsTabError(t *testing.T) {
	old := spawnShell
	spawnShell = func(shell string, args []string, cols, rows int, env pty.Env) (session, error) {
		return &fakeSession{}, nil
	}
	defer func() { spawnShell = old }()
	a := realApp(t, 40, 6, "x\r\n")
	a.clipStubs()
	tt := a.tabs[a.active]
	tt.sess = &fakeSession{}
	a.sess = tt.sess
	tt.err.Store(ptrError(errors.New("previous session died")))
	a.restartShell()
	if tt.err.Load() != nil {
		t.Fatal(":shell restart kept the previous session's read error")
	}
}

// ptrError is a helper adapting an error to tabState.err's pointer slot.
func ptrError(e error) *error { return &e }

// Two tabs exiting with read errors of different concrete types must not
// panic the reaper: a.err used to be an atomic.Value, which panics when
// stores mix concrete types, and read errors from different sessions are
// under no obligation to share one.
func TestReapTabsMixedErrorTypes(t *testing.T) {
	a := realApp(t, 40, 6, "x\r\n")
	a.quit = make(chan struct{})
	t1 := a.tabs[a.active]
	t2 := newTabState(&fakeSession{}, t1.emu, t1.rows)
	a.tabs = append(a.tabs, t2)

	t1.err.Store(ptrError(io.ErrClosedPipe)) // concrete: *errors.errorString
	close(t1.done)
	a.reapTabs()

	t2.err.Store(ptrError(&timeoutError{})) // concrete: *timeoutError, different from above
	close(t2.done)
	a.reapTabs()

	if got := a.childError(); got == nil {
		t.Fatal("reapTabs dropped the recorded read error")
	}
	select {
	case <-a.quit:
	default:
		t.Fatal("closing every tab did not quit the app")
	}
}

// timeoutError is a distinct concrete error type for the mixed-type test.
type timeoutError struct{}

func (*timeoutError) Error() string { return "timeout" }
