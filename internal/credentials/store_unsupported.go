//go:build !windows

package credentials

import "runtime"

// NewPlatformStore returns an honest unavailable store until the platform's
// native keyring adapter is implemented.
func NewPlatformStore() Store {
	return NewUnavailableStore(runtime.GOOS + " native keyring is not implemented")
}

// PlatformStatus reports that this build has no native backend.
func PlatformStatus() (bool, string) {
	return false, runtime.GOOS + " native keyring unavailable"
}
