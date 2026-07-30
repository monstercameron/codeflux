package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionReportsEveryIdentityField(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"version"})
	if code != exitOK {
		t.Fatalf("version exit = %d, want %d; stderr=%q", code, exitOK, stderr.String())
	}
	for _, field := range []string{
		"version:",
		"commit:",
		"build-date:",
		"go-version:",
		"schema-version:",
		"frontend-version:",
	} {
		if !strings.Contains(stdout.String(), field) {
			t.Errorf("version output omits %q: %s", field, stdout.String())
		}
	}
}

func TestDoctorIsHonestAboutUnavailableSubsystems(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"doctor"})
	if code != exitUnavailable {
		t.Fatalf("doctor exit = %d, want %d", code, exitUnavailable)
	}
	for _, subsystem := range []string{"storage: unavailable", "credential-store: unavailable", "browser-transport: unavailable"} {
		if !strings.Contains(stdout.String(), subsystem) {
			t.Errorf("doctor output omits %q: %s", subsystem, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("doctor stderr = %q, want empty", stderr.String())
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"unknown"})
	if code != exitUsage {
		t.Fatalf("unknown exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), `unknown command "unknown"`) {
		t.Fatalf("unknown stderr = %q", stderr.String())
	}
}
