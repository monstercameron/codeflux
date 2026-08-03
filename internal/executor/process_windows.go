//go:build windows

package executor

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func prepareProcessTree(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}
}

func terminateProcessTree(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	kill := exec.Command(
		"taskkill.exe",
		"/PID",
		strconv.Itoa(command.Process.Pid),
		"/T",
		"/F",
	)
	if err := kill.Run(); err != nil {
		return command.Process.Kill()
	}
	return nil
}

// processConfinement (PIPE-132a) is the per-process containment a mediated
// command's OS process is placed under once it starts, closing a gap
// PIPE-131's own comment in agent_verification_exec.go names directly:
// routing a command through ExecuteAuthorizedTool bounds where and how a
// process launches, but nothing bounded what it -- or anything it went on
// to spawn -- could still be doing, or still running as, after the mediated
// call itself considered the command finished.
//
// What this closes, concretely: terminateProcessTree above (the
// taskkill-based tree kill) only ever runs from command.go's timeout and
// cancellation branches. A command that exits zero on its own -- the
// ordinary case, and every acceptance/adversarial/mutation run that
// actually passes -- never had its descendants reaped at all: a background
// helper process started with Start() rather than Wait() kept running,
// unconfined, for as long as it liked, with nothing in this codebase aware
// it existed. A Windows Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
// closes that gap unconditionally: every process ever assigned to the job
// -- the mediated command itself and every descendant it creates, at any
// depth, including one that requests CREATE_BREAKAWAY_FROM_JOB (denied,
// because this job sets neither JOB_OBJECT_LIMIT_BREAKAWAY_OK nor
// _SILENT_BREAKAWAY_OK) -- terminates the instant the job handle closes,
// regardless of whether the OS still considers it a "child" of anything by
// then. command.go now releases the job on every exit path: success,
// failure, fault injection, timeout, and cancellation.
//
// What this does NOT do, stated as directly as PIPE-131's own comment
// states its limit: a Job Object has no bearing on what a still-running
// process does before that job closes. It does not filter which hosts or
// ports the process may connect to, and it does not restrict which
// absolute filesystem paths it may read or write while it is running --
// TestPIPE133_AbuseCaseCredentialNetworkAndFilesystemEscape proves both of
// those remain exactly as unrefused as before this change, because both
// attempts in that fixture complete synchronously, inside the one process
// this job cannot yet have reason to close. AGENTS.md's Security and
// Authority Rules are explicit that workspace confinement must never be
// described as a perfect sandbox, and this is not one: it is a real,
// narrow guarantee about what is still running once the mediated call is
// done with a command, nothing broader.
type processConfinement struct {
	job    windows.Handle
	closed bool
}

// beginProcessConfinement creates a Job Object configured to terminate
// every process it ever contains when its last handle closes, and assigns
// the given command's already-started process to it. It must be called
// after command.Start() succeeds and before command.Process.Wait() runs --
// os.Process.WithHandle documents that a process handle is unusable after
// Wait or Release -- and the returned confinement's release must be called
// on every exit path once this package is finished with the process.
// release is idempotent, so callers that must guarantee cleanup on a
// timeout or cancellation path in addition to a final deferred call may
// call it more than once safely.
//
// Assignment happens as soon as Start() returns control to this package,
// not before: os/exec.Cmd offers no Go-reachable way to create a process
// suspended, assign it to a job, and resume it atomically without
// hand-rolling CreateProcess and its thread handle directly. There is
// therefore a small window, between the OS starting the process's first
// thread and this function's AssignProcessToJobObject call returning,
// during which the child runs outside the job. This is a named limitation,
// not a rounding error hidden in the implementation: a process that
// managed to spawn and fully detach a grandchild inside that window would
// leave that grandchild unconfined. It is not the failure mode PIPE-133's
// abuse case exercises (which runs synchronously inside the mediated
// process itself and completes long before this window would matter), and
// the discriminating test for this ticket spawns its helper process from
// the mediated command's own already-running main, which is the realistic
// shape of repository test and build code -- not from a race against its
// own process creation.
func beginProcessConfinement(command *exec.Cmd) (*processConfinement, error) {
	if command.Process == nil {
		return nil, fmt.Errorf("confine process tree: command has not started")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create confinement job object: %w", err)
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure confinement job object: %w", err)
	}
	var assignErr error
	handleErr := command.Process.WithHandle(func(handle uintptr) {
		assignErr = windows.AssignProcessToJobObject(job, windows.Handle(handle))
	})
	if handleErr != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("obtain process handle to confine: %w", handleErr)
	}
	if assignErr != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("assign process to confinement job object: %w", assignErr)
	}
	return &processConfinement{job: job}, nil
}

// release closes the confinement job object, which terminates every
// process still assigned to it -- the mediated command's own process if it
// is somehow still running, and any descendant it created at any depth --
// per JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE. Safe to call more than once and
// safe to call after the mediated command's own process has already
// exited; closing the job then has nothing left to terminate.
func (confinement *processConfinement) release() error {
	if confinement == nil || confinement.closed {
		return nil
	}
	confinement.closed = true
	return windows.CloseHandle(confinement.job)
}
