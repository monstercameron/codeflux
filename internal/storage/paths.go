package storage

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const databaseFileName = "codeflux.sqlite3"

// DefaultDatabasePath returns the per-user operating-system application-data
// location for the authoritative Codeflux database.
func DefaultDatabasePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return defaultDatabasePath(runtime.GOOS, os.Getenv, home, config)
}

func defaultDatabasePath(
	goos string,
	getenv func(string) string,
	home string,
	config string,
) (string, error) {
	var directory string
	switch goos {
	case "windows":
		directory = getenv("LOCALAPPDATA")
		if directory == "" {
			directory = config
		}
		directory = joinWindowsPath(directory, "Codeflux")
	case "darwin":
		directory = path.Join(home, "Library", "Application Support", "Codeflux")
	case "linux":
		directory = getenv("XDG_DATA_HOME")
		if directory == "" {
			directory = path.Join(home, ".local", "share")
		}
		directory = path.Join(directory, "codeflux")
	default:
		directory = path.Join(config, "codeflux")
	}
	if !isAbsoluteForOS(goos, directory) {
		return "", fmt.Errorf("application-data directory must be absolute: %s", directory)
	}
	if goos == "windows" {
		return joinWindowsPath(directory, databaseFileName), nil
	}
	return path.Join(directory, databaseFileName), nil
}

func joinWindowsPath(elements ...string) string {
	joined := strings.TrimRight(elements[0], `\/`)
	for _, element := range elements[1:] {
		joined += `\` + strings.Trim(element, `\/`)
	}
	return joined
}

func isAbsoluteForOS(goos, candidate string) bool {
	if goos != "windows" {
		return strings.HasPrefix(candidate, "/")
	}
	if strings.HasPrefix(candidate, `\\`) {
		return true
	}
	return len(candidate) >= 3 &&
		((candidate[0] >= 'A' && candidate[0] <= 'Z') ||
			(candidate[0] >= 'a' && candidate[0] <= 'z')) &&
		candidate[1] == ':' &&
		(candidate[2] == '\\' || candidate[2] == '/')
}

func ensureDatabaseFile(path string) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return classify("create application-data directory", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(directory, 0o700); err != nil {
			return classify("restrict application-data directory", err)
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return classify("create database file", err)
	}
	if closeErr := file.Close(); closeErr != nil {
		return classify("close created database file", closeErr)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			return classify("restrict database file", err)
		}
	}
	return nil
}
