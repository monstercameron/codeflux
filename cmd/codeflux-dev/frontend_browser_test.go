package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/frontendtest"
)

func TestBrowserCommandIsHonestlyUnavailableWithoutRunningFrontend(t *testing.T) {
	t.Setenv(frontendURLEnvironment, "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runBrowserTests(
		context.Background(),
		&stdout,
		&stderr,
		commandInvocation{JSON: true, Root: t.TempDir()},
	)

	if code != exitUnavailable {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status":"unavailable"`) ||
		!strings.Contains(stdout.String(), frontendURLEnvironment) {
		t.Fatalf("unavailable JSON=%q", stdout.String())
	}
}

func TestBrowserCommandRejectsNonLoopbackURLBeforeRunner(t *testing.T) {
	t.Setenv(frontendURLEnvironment, "https://example.com:443")
	original := runFrontendSuite
	runFrontendSuite = func(context.Context, frontendtest.Config) (frontendtest.Result, error) {
		t.Fatal("runner called for non-loopback URL")
		return frontendtest.Result{}, nil
	}
	t.Cleanup(func() {
		runFrontendSuite = original
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runBrowserTests(
		context.Background(),
		&stdout,
		&stderr,
		commandInvocation{Root: t.TempDir()},
	)

	if code != exitUsage || !strings.Contains(stderr.String(), "loopback") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestBrowserCommandReportsPassingMachineResult(t *testing.T) {
	t.Setenv(frontendURLEnvironment, "http://127.0.0.1:49152")
	original := runFrontendSuite
	runFrontendSuite = func(_ context.Context, config frontendtest.Config) (frontendtest.Result, error) {
		if config.BaseURL != os.Getenv(frontendURLEnvironment) {
			t.Fatalf("base URL=%q", config.BaseURL)
		}
		return frontendtest.Result{
			SchemaVersion: 1,
			Status:        "passed",
			BaseURL:       config.BaseURL,
			Checks:        []frontendtest.CheckResult{{ID: "fixture", Passed: true}},
		}, nil
	}
	t.Cleanup(func() {
		runFrontendSuite = original
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runBrowserTests(
		context.Background(),
		&stdout,
		&stderr,
		commandInvocation{JSON: true, Root: t.TempDir()},
	)

	if code != exitSuccess || !strings.Contains(stdout.String(), `"status":"passed"`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestBrowserCommandMapsMissingBrowserToUnavailable(t *testing.T) {
	t.Setenv(frontendURLEnvironment, "http://127.0.0.1:49152")
	original := runFrontendSuite
	runFrontendSuite = func(_ context.Context, config frontendtest.Config) (frontendtest.Result, error) {
		return frontendtest.Result{
			SchemaVersion: 1,
			Status:        "unavailable",
			BaseURL:       config.BaseURL,
		}, frontendtest.ErrBrowserUnavailable
	}
	t.Cleanup(func() {
		runFrontendSuite = original
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runBrowserTests(
		context.Background(),
		&stdout,
		&stderr,
		commandInvocation{JSON: true, Root: t.TempDir()},
	)

	if code != exitUnavailable || !strings.Contains(stdout.String(), `"status":"unavailable"`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
