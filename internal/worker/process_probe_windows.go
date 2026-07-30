//go:build windows

package worker

import (
	"errors"

	"golang.org/x/sys/windows"
)

const windowsProcessStillActive = 259

func ProcessAlive(processID int) (bool, error) {
	if processID < 1 {
		return false, errors.New("process ID must be positive")
	}
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(processID),
	)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false, err
	}
	return exitCode == windowsProcessStillActive, nil
}
