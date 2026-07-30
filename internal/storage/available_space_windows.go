//go:build windows

package storage

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func operatingSystemAvailableBytes(path string) (uint64, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("encode filesystem path: %w", err)
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(pointer, &available, nil, nil); err != nil {
		return 0, fmt.Errorf("query filesystem capacity: %w", err)
	}
	return available, nil
}
