package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/x/xpty"
	"golang.org/x/sys/windows"
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

	// On Windows, ConPTY's console output code page does not default to
	// UTF-8: it typically inherits the OEM/ANSI codepage (e.g. 437 or
	// 1252). Programs that print UTF-8-encoded text (nerd-font glyphs in
	// a shell prompt, box-drawing characters, etc.) will then have their
	// bytes reinterpreted under that legacy codepage, producing mojibake
	// like "Ôëí" for what should be "" once our emulator parses the
	// stream as UTF-8. Forcing the codepage to 65001 (UTF-8) before the
	// child's own initialization runs (profile scripts, prompt themes)
	// fixes this at the source instead of trying to patch it up after
	// decoding.
	if runtime.GOOS == "windows" {
		program, args = wrapForUTF8(program, args)
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
	n, err := s.pty.Read(p)
	return n, mapReadErr(err)
}

// mapReadErr translates pipe teardown errors into io.EOF: closing the
// ConPTY surfaces to a blocked reader as ERROR_INVALID_HANDLE (the pipe
// handles are gone) or, depending on teardown order, as ERROR_BROKEN_PIPE
// or ERROR_NO_DATA from ReadFile — never as a clean EOF. A closed or dead
// session has no retry path, so callers treat all three as the normal end
// of the session's output.
func mapReadErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, windows.ERROR_BROKEN_PIPE) ||
		errors.Is(err, windows.ERROR_NO_DATA) ||
		errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		return io.EOF
	}
	return err
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

// wrapForUTF8 rewrites program/args so the child's console output code page
// is switched to UTF-8 (65001) as its first action, before any profile
// script or prompt theme has a chance to print non-ASCII output under the
// wrong legacy codepage. Different shells need different incantations, so
// we branch on the base program name; anything unrecognized is left as-is
// (e.g. wsl.exe, which runs a Linux userspace that already talks UTF-8).
func wrapForUTF8(program string, args []string) (string, []string) {
	base := strings.ToLower(filepath.Base(program))
	base = strings.TrimSuffix(base, filepath.Ext(base))

	switch base {
	case "powershell", "pwsh":
		// Set both the raw console codepage and .NET's Console.OutputEncoding
		// (PowerShell's own Write-Host/Write-Output path uses the latter),
		// then hand off to an interactive shell so profile/prompt output
		// that follows is correctly encoded. User-supplied args (e.g.
		// -NoProfile) must come before the wrapper, or the shell would
		// treat them as part of the -Command string.
		init := "chcp 65001 > $null; " +
			"[Console]::OutputEncoding = [Text.UTF8Encoding]::new(); " +
			"[Console]::InputEncoding = [Text.UTF8Encoding]::new()"
		newArgs := append(append([]string(nil), args...), "-NoExit", "-Command", init)
		return program, newArgs
	case "cmd":
		// /K keeps the shell open after running the codepage switch, then
		// falls through to an interactive prompt. User-supplied args come
		// first so cmd does not treat them as part of the /K command.
		cmdLine := "chcp 65001>nul"
		newArgs := append(append([]string(nil), args...), "/K", cmdLine)
		return program, newArgs
	default:
		return program, args
	}
}

var _ io.ReadWriteCloser = (*Session)(nil)