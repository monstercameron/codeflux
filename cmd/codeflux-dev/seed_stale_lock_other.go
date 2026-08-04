//go:build !windows

package main

// killProcessByPID and killProcessesByExecutablePath have no implementation
// outside Windows: REPO-041c's failure -- os.RemoveAll refusing a file a
// still-live process has open -- is a Windows file-locking behavior. POSIX
// allows unlinking a file a process still has open (the inode survives until
// the last handle closes), so the same hard-kill scenario does not leave
// os.RemoveAll refusing to proceed on Linux or macOS, which is also where
// this repository's own CI runs (AGENTS.md's test-browser platform note).
// A no-op here is therefore honest rather than a gap: there is nothing this
// sweep needs to reclaim on these platforms for the failure it exists to fix.
func killProcessByPID(pid int, expectedExecutablePath string) (killed bool, err error) {
	return false, nil
}

func killProcessesByExecutablePath(path string) (count int, err error) {
	return 0, nil
}
