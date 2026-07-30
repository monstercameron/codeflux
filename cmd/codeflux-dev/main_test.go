package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), &stdout, &stderr, []string{"--help"})

	if code != exitSuccess {
		t.Fatalf("run help exit = %d, want %d; stderr=%q", code, exitSuccess, stderr.String())
	}
	for _, command := range []string{"build", "test-fast"} {
		if !strings.Contains(stdout.String(), command) {
			t.Errorf("help omits command %q: %s", command, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("help stderr = %q, want empty", stderr.String())
	}
}

func TestRunWithoutCommandIsUsageError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), &stdout, &stderr, nil)

	if code != exitUsage {
		t.Fatalf("run without command exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("usage error omits help: %q", stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), &stdout, &stderr, []string{"unknown"})

	if code != exitUsage {
		t.Fatalf("unknown command exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), `unknown command "unknown"`) {
		t.Fatalf("unknown command error = %q", stderr.String())
	}
}
