//go:build !windows

package storage

import "os"

// restrictToCurrentUser narrows the path to its owner.
//
// On these platforms the POSIX mode is the mechanism, and it is applied
// explicitly rather than left to the creating call's mode argument, because
// that argument is masked by umask and a permissive umask would otherwise
// widen the database silently.
func restrictToCurrentUser(path string) error {
	information, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return classify("inspect path before restricting it", err)
	}
	mode := os.FileMode(0o600)
	if information.IsDir() {
		mode = 0o700
	}
	if err := os.Chmod(path, mode); err != nil {
		return classify("restrict "+path+" to its owner", err)
	}
	return nil
}
