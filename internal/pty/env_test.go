package pty

// Tests for the child environment construction. The parts worth pinning are
// the truecolor signals and the case-insensitive replace, since a duplicate
// variable on Windows leaves the child's behavior undefined.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// envMap indexes an environment slice, last occurrence of a name winning (the
// same way a Windows child resolves it).
func envMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		out[strings.ToUpper(k)] = v
	}
	return out
}

func TestTerminalEnvSetsTruecolorSignals(t *testing.T) {
	got := envMap(terminalEnv(nil))
	if got["TERM"] != "xterm-256color" {
		t.Errorf("TERM = %q, want xterm-256color", got["TERM"])
	}
	// Tools gate direct 24-bit output on COLORTERM; without it the emulator
	// renders truecolor that nothing downstream ever asks for.
	if got["COLORTERM"] != "truecolor" {
		t.Errorf("COLORTERM = %q, want truecolor", got["COLORTERM"])
	}
}

func TestTerminalEnvKeepsHostVariables(t *testing.T) {
	t.Setenv("VIMTERM_TEST_MARKER", "kept")
	if got := envMap(terminalEnv(nil))["VIMTERM_TEST_MARKER"]; got != "kept" {
		t.Errorf("VIMTERM_TEST_MARKER = %q, want kept", got)
	}
}

func TestTerminalEnvOverridesHostVariable(t *testing.T) {
	t.Setenv("VIMTERM_TEST_OVERRIDE", "old")
	got := terminalEnv(map[string]string{"VIMTERM_TEST_OVERRIDE": "new"})
	m := envMap(got)
	if m["VIMTERM_TEST_OVERRIDE"] != "new" {
		t.Errorf("VIMTERM_TEST_OVERRIDE = %q, want new", m["VIMTERM_TEST_OVERRIDE"])
	}
	// Appending rather than replacing would leave two entries for one name.
	n := 0
	for _, kv := range got {
		if strings.HasPrefix(strings.ToUpper(kv), "VIMTERM_TEST_OVERRIDE=") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("variable appears %d times, want exactly 1", n)
	}
}

// Windows treats environment names case-insensitively, so an override spelled
// differently from the inherited one must replace it, not sit beside it.
func TestTerminalEnvOverridesCaseInsensitively(t *testing.T) {
	t.Setenv("VIMTERM_TEST_CASE", "old")
	got := terminalEnv(map[string]string{"vimterm_test_case": "new"})
	m := envMap(got)
	if m["VIMTERM_TEST_CASE"] != "new" {
		t.Errorf("VIMTERM_TEST_CASE = %q, want new", m["VIMTERM_TEST_CASE"])
	}
	n := 0
	for _, kv := range got {
		if strings.HasPrefix(strings.ToUpper(kv), "VIMTERM_TEST_CASE=") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("variable appears %d times, want exactly 1", n)
	}
}

// An override must win over the defaults too, otherwise a user could not
// correct a wrong TERM.
func TestTerminalEnvOverridesDefaults(t *testing.T) {
	got := envMap(terminalEnv(map[string]string{"TERM": "screen-256color"}))
	if got["TERM"] != "screen-256color" {
		t.Errorf("TERM = %q, want the override", got["TERM"])
	}
	if got["COLORTERM"] != "truecolor" {
		t.Errorf("COLORTERM = %q, still want truecolor", got["COLORTERM"])
	}
}

func TestTerminalEnvIgnoresEmptyName(t *testing.T) {
	// An empty key would produce a malformed "=value" entry.
	got := terminalEnv(map[string]string{"": "orphan"})
	for _, kv := range got {
		if strings.HasPrefix(kv, "=") {
			t.Errorf("malformed entry %q in child environment", kv)
		}
	}
}

// setEnv must not duplicate an entry when the value already matches.
func TestSetEnvIdempotent(t *testing.T) {
	env := []string{"A=1", "B=2"}
	out := setEnv(env, "A", "1")
	if len(out) != 2 {
		t.Errorf("len = %d, want 2 (unchanged)", len(out))
	}
	out = setEnv(out, "A", "9")
	if len(out) != 2 || envMap(out)["A"] != "9" {
		t.Errorf("setEnv did not replace in place: %+v", out)
	}
}

func TestSetEnvAppendsWhenAbsent(t *testing.T) {
	out := setEnv([]string{"A=1"}, "B", "2")
	if len(out) != 2 || envMap(out)["B"] != "2" {
		t.Errorf("setEnv = %+v, want B=2 appended", out)
	}
}

// readUntil reads the session's output until want appears or the deadline
// passes. Read blocks with no timeout of its own, so it runs on its own
// goroutine and the caller selects on a deadline; the goroutine is left to
// finish when the session is closed by the test's defer.
func readUntil(t *testing.T, sess *Session, want string, within time.Duration) string {
	t.Helper()
	chunks := make(chan string, 256)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				chunks <- string(buf[:n])
			}
			if err != nil {
				close(chunks)
				return
			}
		}
	}()

	var out strings.Builder
	timer := time.NewTimer(within)
	defer timer.Stop()
	for {
		select {
		case c, ok := <-chunks:
			if !ok {
				return out.String()
			}
			out.WriteString(c)
			if strings.Contains(out.String(), want) {
				return out.String()
			}
		case <-timer.C:
			return out.String()
		}
	}
}

// probeEnd marks the end of a probe's output, so the reader waits for a
// complete result rather than a half-written line.
const probeEnd = "VIMPROBE-END"

// spawnProbe runs a PowerShell script that prints one tagged line per value
// and returns the session. PowerShell is used because cmd.exe's /C argument
// does not compose with the UTF-8 wrapper Spawn appends.
func spawnProbe(t *testing.T, script string, env Env) *Session {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.ps1")
	// The end marker is what the reader waits for: waiting on the value's own
	// prefix would match mid-line, before the value is actually emitted.
	body := strings.Join([]string{
		`$ErrorActionPreference = "Continue"`,
		script,
		`Write-Output "VIMPROBE-END"`,
		`exit`,
	}, "\r\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, err := SpawnWithEnv(
		"powershell.exe",
		[]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path},
		80, 25, env,
	)
	if err != nil {
		t.Skipf("spawn: %v", err)
	}
	return sess
}

// A real spawn must see the configured environment, or the setting is inert.
func TestSpawnWithEnvAppliesEnv(t *testing.T) {
	sess := spawnProbe(t, `Write-Output "VIMTEST=[$env:VIMTERM_TEST_ENV]"`,
		Env{Vars: map[string]string{"VIMTERM_TEST_ENV": "from-config"}})
	defer sess.Kill()
	defer sess.Close()

	out := readUntil(t, sess, probeEnd, 30*time.Second)
	if !strings.Contains(out, "VIMTEST=[from-config]") {
		t.Errorf("configured env not applied to child: output %q", out)
	}
}

// The child must be told the terminal is truecolor: this is the whole point of
// setting COLORTERM, and it is invisible in vimterm's own rendering. The
// inherited value is cleared first so an ambient COLORTERM cannot make this
// pass for the wrong reason.
func TestSpawnWithEnvAnnouncesTruecolor(t *testing.T) {
	t.Setenv("COLORTERM", "")
	sess := spawnProbe(t, `Write-Output "VIMTEST=[$env:COLORTERM]"`, Env{})
	defer sess.Kill()
	defer sess.Close()

	out := readUntil(t, sess, probeEnd, 30*time.Second)
	if !strings.Contains(out, "VIMTEST=[truecolor]") {
		t.Errorf("COLORTERM not announced to child: output %q", out)
	}
}

// A configured override must win inside the child, not just in the slice.
func TestSpawnWithEnvOverrideWinsOverInherited(t *testing.T) {
	t.Setenv("VIMTERM_TEST_ENV", "inherited")
	sess := spawnProbe(t, `Write-Output "VIMTEST=[$env:VIMTERM_TEST_ENV]"`,
		Env{Vars: map[string]string{"vimterm_test_env": "from-config"}})
	defer sess.Kill()
	defer sess.Close()

	out := readUntil(t, sess, probeEnd, 30*time.Second)
	// One entry per name: a case-differing duplicate would leave the child's
	// value undefined.
	if strings.Count(out, "VIMTEST=") != 1 {
		t.Errorf("duplicate output, child environment is ambiguous: %q", out)
	}
	if !strings.Contains(out, "VIMTEST=[from-config]") {
		t.Errorf("override did not reach child: output %q", out)
	}
}

// The configured working directory must be where the shell actually starts.
func TestSpawnWithEnvAppliesWorkingDir(t *testing.T) {
	dir := t.TempDir()
	sess := spawnProbe(t, `Write-Output "VIMTEST=" + (Get-Location).Path`, Env{Dir: dir})
	defer sess.Kill()
	defer sess.Close()

	out := readUntil(t, sess, probeEnd, 30*time.Second)
	// Compare case-insensitively: Windows paths are not case-sensitive and the
	// shell echoes the path as it resolved it.
	if !strings.Contains(strings.ToLower(out), strings.ToLower(dir)) {
		t.Errorf("child started in the wrong directory: output %q, want %q", out, dir)
	}
}

// With no configured directory the child inherits vimterm's own, which is what
// an unset dir must keep doing.
func TestSpawnWithoutDirInheritsWorkingDir(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Skipf("getwd: %v", err)
	}
	sess := spawnProbe(t, `Write-Output "VIMTEST=" + (Get-Location).Path`, Env{})
	defer sess.Kill()
	defer sess.Close()

	out := readUntil(t, sess, probeEnd, 30*time.Second)
	if !strings.Contains(strings.ToLower(out), strings.ToLower(cwd)) {
		t.Errorf("child did not inherit working directory: output %q, want %q", out, cwd)
	}
}
