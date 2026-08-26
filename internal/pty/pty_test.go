package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestMapReadErr(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want error
	}{
		{"nil", nil, nil},
		{"eof passthrough", io.EOF, io.EOF},
		{"broken pipe", syscall.Errno(windows.ERROR_BROKEN_PIPE), io.EOF},
		{"no data", syscall.Errno(windows.ERROR_NO_DATA), io.EOF},
		{"invalid handle", syscall.Errno(windows.ERROR_INVALID_HANDLE), io.EOF},
		{
			"wrapped broken pipe",
			fmt.Errorf("read: %w", syscall.Errno(windows.ERROR_BROKEN_PIPE)),
			io.EOF,
		},
		{"other errno kept", syscall.Errno(5), syscall.Errno(5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapReadErr(tt.in)
			if !errors.Is(got, tt.want) || (got == nil) != (tt.want == nil) {
				t.Fatalf("mapReadErr(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestReadEOFAfterClose drives a real ConPTY: tearing down the session
// (what Kill+Close do on tab close / restart) must surface to a blocked
// reader as io.EOF. A raw ERROR_BROKEN_PIPE here used to poison the
// session's stored read error and make the app report a bogus "child read"
// failure on clean exits.
func TestReadEOFAfterClose(t *testing.T) {
	sess, err := Spawn("cmd.exe", []string{"/C", "exit"}, 80, 25)
	if err != nil {
		t.Skipf("spawn: %v", err)
	}

	exited := make(chan struct{})
	go func() {
		_ = sess.Wait(context.Background())
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(15 * time.Second):
		t.Fatal("child did not exit")
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	errc := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := sess.Read(buf); err != nil {
				errc <- err
				return
			}
		}
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Read after close = %v, want io.EOF", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for EOF after close")
	}
}
