package pty

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/charmbracelet/x/xpty"
)

// Session wraps a ConPTY-backed child process (e.g. PowerShell, cmd, wsl).
type Session struct {
	pty  xpty.Pty
	cmd  *exec.Cmd
	cols int
	rows int
}

// Spawn starts a shell in a new ConPTY of the given size.
func Spawn(program string, args []string, cols, rows int) (*Session, error) {
	if program == "" {
		program = "powershell.exe"
	}
	p, err := xpty.NewPty(cols, rows)
	if err != nil {
		return nil, fmt.Errorf("pty: create: %w", err)
	}

	cmd := exec.Command(program, args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	if err := p.Start(cmd); err != nil {
		p.Close()
		return nil, fmt.Errorf("pty: start %s: %w", program, err)
	}

	return &Session{pty: p, cmd: cmd, cols: cols, rows: rows}, nil
}

// Read reads output from the child process.
func (s *Session) Read(p []byte) (int, error) {
	return s.pty.Read(p)
}

// Write writes input to the child process.
func (s *Session) Write(p []byte) (int, error) {
	return s.pty.Write(p)
}

// Resize changes the ConPTY size in cells.
func (s *Session) Resize(cols, rows int) error {
	if err := s.pty.Resize(cols, rows); err != nil {
		return err
	}
	s.cols, s.rows = cols, rows
	return nil
}

// Size returns the current ConPTY size.
func (s *Session) Size() (int, int) {
	return s.cols, s.rows
}

// Wait blocks until the child process exits, or the context is cancelled.
func (s *Session) Wait(ctx context.Context) error {
	return xpty.WaitProcess(ctx, s.cmd)
}

// Kill terminates the child process.
func (s *Session) Kill() error {
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

// Close releases the PTY resources.
func (s *Session) Close() error {
	return s.pty.Close()
}

// Name returns the child program name.
func (s *Session) Name() string {
	return s.cmd.Args[0]
}

var _ io.ReadWriteCloser = (*Session)(nil)