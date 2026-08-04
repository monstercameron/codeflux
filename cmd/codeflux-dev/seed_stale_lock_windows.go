//go:build windows

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// killProcessByPID terminates pid if, and only if, it is still alive and its
// current executable image path exactly matches expectedExecutablePath
// (case-insensitively, Windows paths). This is the exact-ExecutablePath
// verification AGENTS.md requires before a sweep kills anything: without it,
// a PID a hard kill freed and the operating system has since reassigned to
// an unrelated process would be killed on the strength of a stale number
// alone.
//
// It reports killed=false, err=nil for a PID that is not running, or is
// running a different executable, rather than treating either as an error:
// both are the ordinary case of a previous run that already exited cleanly
// or a PID long since recycled.
func killProcessByPID(pid int, expectedExecutablePath string) (killed bool, err error) {
	actual, ok := executablePathForPID(uint32(pid))
	if !ok || !strings.EqualFold(actual, expectedExecutablePath) {
		return false, nil
	}
	kill := exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F")
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := kill.Run(); err != nil {
		return false, fmt.Errorf("taskkill pid %d: %w", pid, err)
	}
	return true, nil
}

// killProcessesByExecutablePath terminates every currently running process
// whose full executable image path exactly matches path (case-insensitively).
// Unlike killProcessByPID this needs no recorded PID at all: the worker's
// build output path is deterministic given root, so this call alone reclaims
// every orphan left by any number of past hard kills, not only the most
// recent one a pidfile could have named.
func killProcessesByExecutablePath(path string) (count int, err error) {
	pids, err := findProcessesByExecutablePath(path)
	if err != nil {
		return 0, err
	}
	for _, pid := range pids {
		kill := exec.Command("taskkill.exe", "/PID", strconv.Itoa(int(pid)), "/T", "/F")
		kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if killErr := kill.Run(); killErr != nil {
			if err == nil {
				err = fmt.Errorf("taskkill pid %d: %w", pid, killErr)
			}
			continue
		}
		count++
	}
	return count, err
}

// findProcessesByExecutablePath enumerates every running process via a
// CreateToolhelp32Snapshot and returns the PIDs whose resolved image path
// matches target exactly. PROCESSENTRY32's own ExeFile field is only a base
// name, not a full path, so each candidate's full path is resolved
// individually through OpenProcess + QueryFullProcessImageName before it
// counts as a match.
func findProcessesByExecutablePath(target string) ([]uint32, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("snapshot processes: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var matches []uint32
	if err := windows.Process32First(snapshot, &entry); err != nil {
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return matches, nil
		}
		return nil, fmt.Errorf("enumerate processes: %w", err)
	}
	for {
		if path, ok := executablePathForPID(entry.ProcessID); ok && strings.EqualFold(path, target) {
			matches = append(matches, entry.ProcessID)
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return matches, nil
}

// executablePathForPID resolves one process's full executable image path.
// It returns ok=false for a PID that is not running, or that this process
// lacks permission to query -- both are treated as "not a match" by every
// caller here, never as a reason to kill something it could not verify.
func executablePathForPID(pid uint32) (string, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", false
	}
	return windows.UTF16ToString(buffer[:size]), true
}
