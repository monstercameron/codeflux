//go:build !windows

package worker

import (
	"errors"
	"syscall"
)

func ProcessAlive(processID int) (bool, error) {
	if processID < 1 {
		return false, errors.New("process ID must be positive")
	}
	err := syscall.Kill(processID, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return false, err
}
