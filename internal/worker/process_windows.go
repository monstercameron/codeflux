//go:build windows

package worker

import (
	"errors"
	"os/exec"
	"strconv"
	"syscall"
)

func prepareProcessTree(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func terminateProcessTree(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	kill := exec.Command(
		"taskkill.exe", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F",
	)
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := kill.Run(); err == nil {
		return nil
	}
	if err := command.Process.Kill(); err != nil &&
		!errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return err
	}
	return nil
}
