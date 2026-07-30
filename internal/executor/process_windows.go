//go:build windows

package executor

import (
	"os/exec"
	"strconv"
	"syscall"

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
