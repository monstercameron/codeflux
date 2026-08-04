//go:build windows

package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// buildSleeperExecutable compiles a tiny standalone binary that sleeps far
// longer than any test here runs, so a test can start a real, independently
// killable process at a known, exact executable path -- the same thing
// codeflux-worker.exe is to a real seed --serve run, without needing the
// whole coordinator to reproduce REPO-041c's failure.
func buildSleeperExecutable(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "sleeper.go")
	if err := os.WriteFile(src, []byte(
		"package main\n\nimport \"time\"\n\nfunc main() { time.Sleep(5 * time.Minute) }\n"),
		0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "sleeper.exe")
	build := exec.Command("go", "build", "-o", out, src)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sleeper fixture: %v\n%s", err, output)
	}
	return out
}

// startSleeper starts one instance of the sleeper fixture and arranges for
// it to be killed at test end even if the test's own assertions never get
// the chance.
func startSleeper(t *testing.T, exePath string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(exePath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// waitForProcessGone polls until executablePathForPID no longer resolves
// pid, or fails the test after a bounded wait -- termination is asynchronous
// (taskkill returns once the request is issued, not once the OS has fully
// torn the process down), so a single immediate check would flake.
func waitForProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := executablePathForPID(uint32(pid)); !ok {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("pid %d is still running after the sweep", pid)
}

// TestSweepStaleSeedLockKillsAnAliveCoordinatorRecordedByExactPath proves the
// core REPO-041c mechanism: a lock file naming a still-alive PID whose
// current executable image matches the recorded path is reclaimed.
//
// Discrimination: this is the manual reproduction already run once by hand
// (two real `seed --serve` invocations, the second one printing "reclaimed a
// stale coordinator" and successfully proceeding past os.RemoveAll(root)
// where the unswept version fails with "Access is denied"), captured here as
// an automated test so it does not depend on a person repeating that by
// hand. Removing the killProcessByPID call from sweepStaleSeedLock (or
// passing it the wrong path) makes this test fail on the waitForProcessGone
// timeout, because the sleeper would still be running.
func TestSweepStaleSeedLockKillsAnAliveCoordinatorRecordedByExactPath(t *testing.T) {
	exePath := buildSleeperExecutable(t)
	sleeper := startSleeper(t, exePath)
	pid := sleeper.Process.Pid

	root := filepath.Join(t.TempDir(), "seed")
	record := seedServeLockRecord{
		CoordinatorPID:            pid,
		CoordinatorExecutablePath: exePath,
		StartedAtUnix:             time.Now().Unix(),
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seedServeLockPath(root), body, 0o600); err != nil {
		t.Fatal(err)
	}

	sweepStaleSeedLock(io.Discard, root)

	waitForProcessGone(t, pid)

	if _, err := os.Stat(seedServeLockPath(root)); !os.IsNotExist(err) {
		t.Errorf("the lock file should be removed once the sweep has processed it, stat error = %v", err)
	}
}

// TestSweepStaleSeedLockNeverKillsAPIDWhoseExecutableHasChanged proves the
// exact-ExecutablePath safety guard AGENTS.md requires: a PID that is alive
// but running a *different* executable than the one recorded must never be
// killed, because that is exactly the shape of an operating system having
// reused a freed PID for something this command never started.
//
// Discrimination: dropping the strings.EqualFold comparison in
// killProcessByPID (killing on PID match alone) makes this test fail, since
// the sleeper -- a real, unrelated process from this command's point of view
// -- would then be killed despite the recorded path naming something else
// entirely.
func TestSweepStaleSeedLockNeverKillsAPIDWhoseExecutableHasChanged(t *testing.T) {
	exePath := buildSleeperExecutable(t)
	sleeper := startSleeper(t, exePath)
	pid := sleeper.Process.Pid

	root := filepath.Join(t.TempDir(), "seed")
	record := seedServeLockRecord{
		CoordinatorPID:            pid,
		CoordinatorExecutablePath: filepath.Join(t.TempDir(), "not-the-real-binary.exe"),
		StartedAtUnix:             time.Now().Unix(),
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seedServeLockPath(root), body, 0o600); err != nil {
		t.Fatal(err)
	}

	sweepStaleSeedLock(io.Discard, root)

	if _, ok := executablePathForPID(uint32(pid)); !ok {
		t.Fatal("the sweep killed a PID whose recorded path did not match its real executable")
	}
}

// TestKillProcessesByExecutablePathReclaimsEveryMatchingOrphan proves the
// worker-path sweep (no pidfile involved at all) finds and terminates every
// live process at the given path, which is what lets the sweep reclaim a
// codeflux-worker.exe orphan left by any number of past hard kills, not only
// the most recent one a single pidfile could name.
func TestKillProcessesByExecutablePathReclaimsEveryMatchingOrphan(t *testing.T) {
	exePath := buildSleeperExecutable(t)
	first := startSleeper(t, exePath)
	second := startSleeper(t, exePath)

	count, err := killProcessesByExecutablePath(exePath)
	if err != nil {
		t.Fatalf("killProcessesByExecutablePath returned an error: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	waitForProcessGone(t, first.Process.Pid)
	waitForProcessGone(t, second.Process.Pid)
}

// TestKillProcessesByExecutablePathIgnoresUnrelatedPaths proves the path
// sweep leaves alone a real, currently-running process whose executable path
// simply does not match -- the ordinary case on every invocation that finds
// no stale worker at all.
func TestKillProcessesByExecutablePathIgnoresUnrelatedPaths(t *testing.T) {
	exePath := buildSleeperExecutable(t)
	sleeper := startSleeper(t, exePath)

	count, err := killProcessesByExecutablePath(filepath.Join(t.TempDir(), "no-such-worker.exe"))
	if err != nil {
		t.Fatalf("killProcessesByExecutablePath returned an error: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if _, ok := executablePathForPID(uint32(sleeper.Process.Pid)); !ok {
		t.Fatal("an unrelated running process was killed by a non-matching path")
	}
}
