//go:build !windows

package storage

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func operatingSystemAvailableBytes(path string) (uint64, error) {
	var statistics unix.Statfs_t
	if err := unix.Statfs(path, &statistics); err != nil {
		return 0, fmt.Errorf("query filesystem capacity: %w", err)
	}
	return uint64(statistics.Bavail) * uint64(statistics.Bsize), nil
}
