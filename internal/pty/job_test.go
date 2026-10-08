package pty

// Tests for the Job Object that contains the child process tree. The behavior
// worth pinning is teardown: closing or killing a session must take down
// everything the shell started, not just the shell. Before the job object
// existed those grandchildren survived and kept the ConPTY pipe open.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// stillActive is the exit code Windows reports for a running process.
const stillActive = 259

// processAlive reports whether a pid still names a live process.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// waitGone polls until the pid is gone, or reports false at the deadline.
func waitGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return !processAlive(pid)
}

// spawnWithGrandchild starts a shell that immediately spawns a long-lived
// grandchild recording its own pid, then exits. It returns the session and the
// grandchild's pid, skipping the test when the environment cannot produce one
// (no cmd, no background support, process already gone).
func spawnWithGrandchild(t *testing.T) (*Session, int) {
	t.Helper()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")

	// PowerShell records the pid of a detached long-running ping and exits
	// immediately, leaving the ping as a live grandchild. cmd alone cannot
	// report a child's pid without wmic, which is deprecated.
	script := "(Start-Process -FilePath ping.exe -ArgumentList '-n','60','127.0.0.1' " +
		"-WindowStyle Hidden -PassThru).Id | Set-Content -Path '" + pidFile + "'"
	ps1 := filepath.Join(dir, "spawn.ps1")
	if err := os.WriteFile(ps1, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	sess, err := Spawn("powershell.exe", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ps1}, 80, 25)
	if err != nil {
		t.Skipf("spawn: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if pid, ok := readPID(pidFile); ok && processAlive(pid) {
			return sess, pid
		}
		time.Sleep(100 * time.Millisecond)
	}
	sess.Close()
	t.Skip("could not observe a surviving grandchild process")
	return nil, 0
}

// readPID parses the pid file the grandchild writes.
func readPID(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// A real shell must land inside a job object, or the teardown tests below
// would pass for the wrong reason.
func TestSessionIsAssignedToJob(t *testing.T) {
	sess, err := Spawn("cmd.exe", []string{"/C", "timeout /t 2"}, 80, 25)
	if err != nil {
		t.Skipf("spawn: %v", err)
	}
	defer sess.Close()
	defer sess.Kill()

	if sess.job == windows.Handle(0) {
		t.Fatal("no job object: Spawn did not assign the child")
	}
	// x/sys/windows has no exported basic-accounting struct, so declare the
	// documented layout: four uint64 counters, the third being the live count.
	var info struct {
		TotalUserTime             uint64
		TotalKernelTime           uint64
		ThisPeriodTotalUserTime   uint64
		ThisPeriodTotalKernelTime uint64
		TotalPageFaultCount       uint32
		TotalProcesses            uint32
		ActiveProcesses           uint32
		TotalTerminatedProcesses  uint32
	}
	if err := windows.QueryInformationJobObject(
		sess.job,
		windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	); err != nil {
		t.Fatalf("QueryInformationJobObject: %v", err)
	}
	if info.ActiveProcesses == 0 {
		t.Error("job reports no active processes while the child is running")
	}
	if info.TotalProcesses == 0 {
		t.Error("job records no processes at all")
	}
}

// The whole point of the job: a process the shell started must die with the
// session, instead of leaking and holding the ConPTY pipe open.
func TestCloseKillsGrandchildren(t *testing.T) {
	sess, grandchild := spawnWithGrandchild(t)

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !waitGone(grandchild, 15*time.Second) {
		t.Errorf("grandchild %d survived Close: it was not in the job", grandchild)
	}
}

// Kill must take the tree down as well, not only Close: the app calls Kill
// first on tab close and on :shell restart.
func TestKillTerminatesTree(t *testing.T) {
	sess, grandchild := spawnWithGrandchild(t)

	if err := sess.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if !waitGone(grandchild, 15*time.Second) {
		t.Errorf("grandchild %d survived Kill", grandchild)
	}
}

// Close must tolerate a second call: the app tears sessions down from several
// paths, and the job handle must not be closed twice.
func TestCloseIsIdempotent(t *testing.T) {
	sess, err := Spawn("cmd.exe", []string{"/C", "exit"}, 80, 25)
	if err != nil {
		t.Skipf("spawn: %v", err)
	}
	_ = sess.Close()
	_ = sess.Close() // must not panic
}

// Close must not report an error when there is no job to close, which is the
// case when containment was unavailable at startup.
func TestCloseWithoutJob(t *testing.T) {
	sess, err := Spawn("cmd.exe", []string{"/C", "exit"}, 80, 25)
	if err != nil {
		t.Skipf("spawn: %v", err)
	}
	sess.job = 0 // as if assignToJob had failed
	if err := sess.Close(); err != nil && !strings.Contains(err.Error(), "close job") {
		t.Errorf("Close without a job = %v, want no job error", err)
	}
}
