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
	"unsafe"

	"github.com/charmbracelet/x/xpty"
	"golang.org/x/sys/windows"
)

// Session wraps a ConPTY-backed child process (e.g. PowerShell, cmd, wsl).
type Session struct {
	pty  xpty.Pty
	cmd  *exec.Cmd
	cols int
	rows int

	// job is a Windows Job Object holding the shell and every process it
	// spawns, so closing a tab tears down the whole tree. It is nil when the
	// job could not be created, in which case Kill falls back to killing the
	// direct child only.
	job windows.Handle
}

// Env holds the extra environment entries to set on the child, and Dir the
// working directory to start it in. Both are optional: a zero Env means only
// the terminal-identifying defaults below, and an empty Dir inherits the
// current process's directory. Windows environment variables are
// case-insensitive, so Env is merged with setEnv rather than appended.
type Env struct {
	Vars map[string]string
	Dir  string
}

// Spawn starts a shell in a new ConPTY of the given size.
func Spawn(program string, args []string, cols, rows int) (*Session, error) {
	return SpawnWithEnv(program, args, cols, rows, Env{})
}

// SpawnWithEnv is Spawn with extra environment entries and a working
// directory.
func SpawnWithEnv(program string, args []string, cols, rows int, env Env) (*Session, error) {
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
	// COLORTERM is the conventional signal that a terminal supports
	// direct 24-bit color. Without it a great many tools (bat, delta,
	// fzf, Starship, most prompt themes) silently drop to their 256-color
	// or 16-color approximations, so the emulator's own ability to render
	// truecolor never reaches the output.
	cmd.Env = terminalEnv(env.Vars)
	if env.Dir != "" {
		cmd.Dir = env.Dir
	}

	if err := p.Start(cmd); err != nil {
		p.Close()
		return nil, fmt.Errorf("pty: start %s: %w", program, err)
	}

	s := &Session{pty: p, cmd: cmd, cols: cols, rows: rows}
	// Containment is best effort: losing it means closing a tab leaves
	// grandchildren running, which is worse than a warning but must not stop
	// the shell from launching.
	if err := s.assignToJob(); err != nil {
		debugLog("job object unavailable: %v", err)
	}
	return s, nil
}

// debugLog reports a non-fatal setup problem when VIMTERM_DEBUG_PTY is set.
// vimterm owns the host console in raw mode, so stderr is not reliably visible
// and a warning there would be lost or corrupt the screen.
func debugLog(format string, args ...any) {
	if os.Getenv("VIMTERM_DEBUG_PTY") == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "vimterm: "+format+"\n", args...)
}

// assignToJob puts the child in a Job Object configured to kill every process
// in it when the last handle closes. Without this, killing a tab only reached
// the shell itself: anything it started (an ssh session, an editor, an IDE
// server) survived, kept the ConPTY pipe open, and leaked for the rest of the
// session.
//
// KILL_ON_JOB_CLOSE is what makes Close() authoritative. The handle is owned
// by the Session and closed alongside the PTY, so every path that tears a
// session down (tab close, :shell restart, quitting vimterm) kills the tree
// without needing each caller to remember to.
func (s *Session) assignToJob() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("set limits: %w", err)
	}

	// AssignProcessToJobObject needs a process handle with rights to change
	// the job membership; os.Process does not expose one.
	proc, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(s.cmd.Process.Pid),
	)
	if err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("open child: %w", err)
	}
	defer windows.CloseHandle(proc)

	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("assign: %w", err)
	}
	s.job = job
	return nil
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

// Kill terminates the child process and everything it spawned. The job
// handles the tree; the direct-child kill is a fallback for when no job was
// created, and also covers the case where the child already exited (a job with
// no live processes makes TerminateJobObject fail).
func (s *Session) Kill() error {
	if s.job != windows.Handle(0) {
		if err := windows.TerminateJobObject(s.job, 1); err == nil {
			return nil
		}
	}
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

// Close releases the PTY resources. Closing the job handle with
// KILL_ON_JOB_CLOSE set terminates any surviving descendants, so this is what
// guarantees a closed tab leaves nothing behind.
func (s *Session) Close() error {
	var errs []error
	if s.pty != nil {
		if err := s.pty.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.job != windows.Handle(0) {
		if err := windows.CloseHandle(s.job); err != nil {
			errs = append(errs, fmt.Errorf("close job: %w", err))
		}
		s.job = 0
	}
	return errors.Join(errs...)
}

// Name returns the child program name.
func (s *Session) Name() string {
	return s.cmd.Args[0]
}

// terminalEnv builds the child environment: the host's own variables, then
// the entries identifying this as a truecolor terminal, then the user's
// configured overrides last so they win.
//
// setEnv replaces an existing variable case-insensitively instead of appending,
// because Windows treats environment names case-insensitively: appending
// "Path=..." beside an inherited "PATH=..." leaves the child with two values
// for one name, and which one wins is undefined.
func terminalEnv(overrides map[string]string) []string {
	env := os.Environ()
	set := func(key, value string) {
		env = setEnv(env, key, value)
	}
	set("TERM", "xterm-256color")
	set("COLORTERM", "truecolor")
	for k, v := range overrides {
		if k == "" {
			continue
		}
		set(k, v)
	}
	return env
}

// setEnv returns env with key set to value, replacing any existing entry whose
// name matches case-insensitively. A key that is already present with that
// exact value is left untouched so the slice does not grow needlessly.
func setEnv(env []string, key, value string) []string {
	entry := key + "=" + value
	for i, kv := range env {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || !strings.EqualFold(name, key) {
			continue
		}
		if kv == entry {
			return env
		}
		env[i] = entry
		return env
	}
	return append(env, entry)
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
