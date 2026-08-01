//go:build windows

package main

import (
	"errors"
	"path/filepath"
	"syscall"
	"unsafe"
)

// platformFreeBytes reports free bytes available to the calling user.
//
// GetDiskFreeSpaceExW's first output is the caller's quota-adjusted free
// space, which is the number that matters: on a machine with disk quotas, the
// volume's total free space is not what this process can actually use.
func platformFreeBytes(path string) (uint64, error) {
	if path == "" {
		return 0, errors.New("no path was supplied")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	pointer, err := syscall.UTF16PtrFromString(absolute)
	if err != nil {
		return 0, err
	}

	kernel := syscall.NewLazyDLL("kernel32.dll")
	procedure := kernel.NewProc("GetDiskFreeSpaceExW")
	var freeToCaller, totalBytes, totalFree uint64
	result, _, callErr := procedure.Call(
		uintptr(unsafe.Pointer(pointer)),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if result == 0 {
		return 0, callErr
	}
	return freeToCaller, nil
}
