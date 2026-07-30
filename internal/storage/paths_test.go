package storage

import (
	"testing"
)

func TestDefaultDatabasePathByOperatingSystem(t *testing.T) {
	environment := map[string]string{
		"LOCALAPPDATA":  `C:\Users\fixture\AppData\Local`,
		"XDG_DATA_HOME": "/xdg",
	}
	getenv := func(key string) string { return environment[key] }
	home := "/home/fixture"
	config := "/config"
	tests := map[string]string{
		"windows": `C:\Users\fixture\AppData\Local\Codeflux\codeflux.sqlite3`,
		"darwin":  "/home/fixture/Library/Application Support/Codeflux/codeflux.sqlite3",
		"linux":   "/xdg/codeflux/codeflux.sqlite3",
		"other":   "/config/codeflux/codeflux.sqlite3",
	}
	for goos, want := range tests {
		t.Run(goos, func(t *testing.T) {
			got, err := defaultDatabasePath(goos, getenv, home, config)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
		})
	}
}

func TestLinuxDatabasePathFallsBackToLocalShare(t *testing.T) {
	home := "/home/fixture"
	got, err := defaultDatabasePath(
		"linux",
		func(string) string { return "" },
		home,
		"/config",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "/home/fixture/.local/share/codeflux/codeflux.sqlite3"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}
