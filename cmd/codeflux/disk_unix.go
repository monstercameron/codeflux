//go:build !windows

package main

import (
	"errors"
	"path/filepath"
	"syscall"
)

// platformFreeBytes reports free bytes available to the calling user.
//
// Bavail rather than Bfree: Bfree includes blocks reserved for root, which an
// ordinary process cannot use, and reporting them would tell a user they have
// space they cannot actually write to.
func platformFreeBytes(path string) (uint64, error) {
	if path == "" {
		return 0, errors.New("no path was supplied")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	var statistics syscall.Statfs_t
	if err := syscall.Statfs(absolute, &statistics); err != nil {
		return 0, err
	}
	return uint64(statistics.Bavail) * uint64(statistics.Bsize), nil
}
