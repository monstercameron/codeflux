//go:build windows

package main

import (
	"errors"
	"strings"

	"golang.org/x/sys/windows"
)

func resolveExistingPath(path string) (string, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		pointer,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)

	buffer := make([]uint16, 32768)
	length, err := windows.GetFinalPathNameByHandle(
		handle,
		&buffer[0],
		uint32(len(buffer)),
		0,
	)
	if err != nil {
		return "", err
	}
	if length == 0 || length >= uint32(len(buffer)) {
		return "", errors.New("resolved Windows path exceeds supported bounds")
	}
	resolved := windows.UTF16ToString(buffer[:length])
	switch {
	case strings.HasPrefix(resolved, `\\?\UNC\`):
		resolved = `\\` + strings.TrimPrefix(resolved, `\\?\UNC\`)
	case strings.HasPrefix(resolved, `\\?\`):
		resolved = strings.TrimPrefix(resolved, `\\?\`)
	}
	return resolved, nil
}
