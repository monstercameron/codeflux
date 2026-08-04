package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// seedServeLockRecord is the reclaimable pidfile REPO-041c asks for.
//
// StartMountedThread's own defer Shutdown only ever runs on a clean exit:
// `codeflux-dev seed` under CODEFLUX_DEV_SEED_SERVE spawns a real coordinator
// (this process) and a real detached codeflux-worker.exe subprocess
// (internal/worker deliberately runs it with CREATE_NEW_PROCESS_GROUP on
// Windows, so it survives this process dying). A hard kill of this process
// -- observed three times on 2026-08-02, including from an agent's own
// backgrounded shell losing track of the actual `go run`-compiled binary --
// leaves both still holding open handles under root, and the next seed's
// `os.RemoveAll(root)` fails with "unlinkat ... Access is denied" because
// Windows refuses to delete a file a live process still has open.
//
// The record is written once this run's coordinator is actually up, at a
// path deliberately *outside* root (root itself is wiped by RemoveAll before
// each run, so a record living inside it could never be read back). The next
// invocation reads it back before wiping root and reclaims what it names.
type seedServeLockRecord struct {
	// CoordinatorPID is this process's own PID -- the one holding
	// coordinator.sqlite3 open via its live *sql.DB handle.
	CoordinatorPID int `json:"coordinator_pid"`
	// CoordinatorExecutablePath is this process's own resolved executable
	// image path (os.Executable()), recorded so a later sweep only ever
	// kills a PID that still is this same binary -- never a PID a hard kill
	// freed for reuse and the operating system has since handed to something
	// else entirely (AGENTS.md's "verified by exact ExecutablePath" rule).
	CoordinatorExecutablePath string `json:"coordinator_executable_path"`
	// WorkerExecutablePath is the deterministic path `seed --serve` builds
	// codeflux-worker.exe to. It is a path, not a PID: the worker is spawned
	// deep inside internal/coordinator/internal/worker, which this command
	// does not get a live PID back from, but the build output path is fixed
	// by this command's own root before the worker is ever spawned. A later
	// sweep matches on this exact path rather than guessing at a PID.
	WorkerExecutablePath string `json:"worker_executable_path"`
	StartedAtUnix        int64  `json:"started_at_unix"`
}

// seedServeLockPath is where the record lives: a sibling of root, under
// .artifacts, so it survives the very os.RemoveAll(root) it exists to guard
// the next invocation of.
func seedServeLockPath(root string) string {
	return root + ".lock"
}

// writeSeedServeLock records this run's own PID and the worker path it is
// about to build, before StartMountedThread ever spawns anything, so a hard
// kill mid-run still leaves a reclaimable record rather than none at all.
func writeSeedServeLock(root, workerExecutablePath string) error {
	executable, err := os.Executable()
	if err != nil {
		executable = ""
	}
	record := seedServeLockRecord{
		CoordinatorPID:            os.Getpid(),
		CoordinatorExecutablePath: executable,
		WorkerExecutablePath:      workerExecutablePath,
		StartedAtUnix:             time.Now().Unix(),
	}
	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode seed-serve lock: %w", err)
	}
	if err := os.WriteFile(seedServeLockPath(root), body, 0o600); err != nil {
		return fmt.Errorf("write seed-serve lock: %w", err)
	}
	return nil
}

// clearSeedServeLock removes the record on a clean exit, so a later sweep
// has nothing stale to reclaim. Absence is not an error: nothing wrote one
// yet is the common case on a repository's first ever `seed --serve`.
func clearSeedServeLock(root string) {
	_ = os.Remove(seedServeLockPath(root))
}

// sweepStaleSeedLock is REPO-041c's stale-lock sweep, run before root is
// wiped rather than relying on the previous run's own defer Shutdown.
//
// It reclaims two independent things a hard kill can leave behind: the
// coordinator process itself (by the recorded PID, verified against the
// recorded executable path before anything is killed) and any live
// codeflux-worker.exe still holding the deterministic build output path this
// run is about to overwrite (matched by path, not PID, on the platforms that
// support enumerating processes by executable image -- currently Windows,
// which is also the only platform this failure has been reproduced on; see
// killProcessesByExecutablePath's per-platform implementation).
//
// It is deliberately tolerant: a missing lock file, an already-dead PID, or
// a platform with no enumeration support are all the ordinary case (nothing
// to reclaim) rather than an error. os.RemoveAll(root) right after this call
// is what actually proves whether the sweep worked; this function's job is
// only to try.
func sweepStaleSeedLock(stdout io.Writer, root string) {
	lockPath := seedServeLockPath(root)
	body, err := os.ReadFile(lockPath)
	if err != nil {
		return
	}
	var record seedServeLockRecord
	if err := json.Unmarshal(body, &record); err != nil {
		// A record this command cannot even parse is not one it can safely
		// reclaim by PID; remove it so it does not keep confusing every
		// future invocation, and fall through to the path-matched worker
		// sweep below, which does not depend on it.
		_ = os.Remove(lockPath)
	} else if record.CoordinatorPID > 0 && record.CoordinatorExecutablePath != "" {
		killed, killErr := killProcessByPID(record.CoordinatorPID, record.CoordinatorExecutablePath)
		if killErr != nil {
			fmt.Fprintf(stdout, "codeflux-dev seed: stale-lock sweep: coordinator pid %d: %v\n",
				record.CoordinatorPID, killErr)
		} else if killed {
			fmt.Fprintf(stdout, "codeflux-dev seed: reclaimed a stale coordinator (pid %d) left "+
				"by a hard kill of a previous seed --serve\n", record.CoordinatorPID)
		}
	}
	if record.WorkerExecutablePath != "" {
		count, killErr := killProcessesByExecutablePath(record.WorkerExecutablePath)
		if killErr != nil {
			fmt.Fprintf(stdout, "codeflux-dev seed: stale-lock sweep: worker %s: %v\n",
				record.WorkerExecutablePath, killErr)
		} else if count > 0 {
			fmt.Fprintf(stdout, "codeflux-dev seed: reclaimed %d stale codeflux-worker process(es) "+
				"left by a hard kill of a previous seed --serve\n", count)
		}
	}
	_ = os.Remove(lockPath)
}

// deterministicSeedWorkerPath is the exact path buildSeedWorkerExecutable
// will build to, computed before root is wiped so the stale-lock sweep can
// match a previous run's orphaned worker by path even though this run has
// not built (or spawned) its own yet.
func deterministicSeedWorkerPath(root, executableName string) string {
	return filepath.Join(root, "bin", executableName)
}

// seedWorkerExecutableName is the platform-appropriate codeflux-worker
// binary name. buildSeedWorkerExecutable's build target and the stale-lock
// sweep's deterministic path match both have to agree on this name, so it is
// computed once here rather than duplicated at each call site.
func seedWorkerExecutableName() string {
	name := "codeflux-worker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}
