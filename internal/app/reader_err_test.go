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
	if _, ok := tt.err.Load().(error); !ok {
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
	if _, ok := tt.err.Load().(error); ok {
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
	spawnShell = func(shell string, args []string, cols, rows int) (session, error) {
		return &fakeSession{}, nil
	}
	defer func() { spawnShell = old }()
	a := realApp(t, 40, 6, "x\r\n")
	a.clipStubs()
	tt := a.tabs[a.active]
	tt.sess = &fakeSession{}
	a.sess = tt.sess
	tt.err.Store(errors.New("previous session died"))
	a.restartShell()
	if _, ok := tt.err.Load().(error); ok {
		t.Fatal(":shell restart kept the previous session's read error")
	}
}
