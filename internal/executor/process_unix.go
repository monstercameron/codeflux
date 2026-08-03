//go:build !windows

package executor

import (
	"os/exec"
	"syscall"
)

func prepareProcessTree(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessTree(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
}

// processConfinement is a no-op on non-Windows platforms. PIPE-132a scopes
// the new containment this file's Windows counterpart adds -- a Job Object
// that guarantees every descendant a mediated command spawns is
// terminated, even one that outlives the command's own successful exit --
// to Windows, per the ticket's own framing ("the natural mechanism is a
// Job Object"). It was not verified against a Unix mechanism (cgroups,
// PR_SET_PDEATHSIG) on this Windows development machine, so none is
// claimed here. terminateProcessTree's existing process-group kill above
// is unchanged by this ticket and keeps its existing timeout/cancellation
// call sites in command.go exactly as they were.
type processConfinement struct{}

func beginProcessConfinement(*exec.Cmd) (*processConfinement, error) {
	return &processConfinement{}, nil
}

func (confinement *processConfinement) release() error {
	return nil
}
