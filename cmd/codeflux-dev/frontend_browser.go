package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"codeflux.dev/codeflux/internal/frontendtest"
)

const (
	frontendURLEnvironment        = "CODEFLUX_FRONTEND_URL"
	chromiumExecutableEnvironment = "CODEFLUX_CHROMIUM_EXECUTABLE"
	playwrightDriverEnvironment   = "PLAYWRIGHT_DRIVER_PATH"
)

var runFrontendSuite = frontendtest.Run

func runBrowserTests(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	repository, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev test-browser: repository: %v\n", err)
		return exitFailure
	}
	artifactDir, err := resolveCommandRoot(repository, "test-browser", invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev test-browser: root: %v\n", err)
		return exitUsage
	}
	baseURL := os.Getenv(frontendURLEnvironment)
	if baseURL == "" {
		return writeBrowserUnavailable(
			stdout,
			stderr,
			invocation.JSON,
			"set CODEFLUX_FRONTEND_URL to an already-running loopback frontend origin",
		)
	}
	if _, err := frontendtest.ValidateBaseURL(baseURL); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev test-browser: %v\n", err)
		return exitUsage
	}
	result, runErr := runFrontendSuite(ctx, frontendtest.Config{
		BaseURL:         baseURL,
		ArtifactDir:     artifactDir,
		DriverDirectory: os.Getenv(playwrightDriverEnvironment),
		ExecutablePath:  os.Getenv(chromiumExecutableEnvironment),
		Timeout:         10 * time.Second,
	})
	if invocation.JSON {
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintf(stderr, "codeflux-dev test-browser: encode result: %v\n", err)
			return exitFailure
		}
	}
	if runErr != nil {
		if errors.Is(runErr, frontendtest.ErrBrowserUnavailable) {
			if !invocation.JSON {
				fmt.Fprintf(stderr, "codeflux-dev test-browser: unavailable: %v\n", runErr)
			}
			return exitUnavailable
		}
		fmt.Fprintf(stderr, "codeflux-dev test-browser: %v\n", runErr)
		return exitFailure
	}
	if result.Status != "passed" {
		if !invocation.JSON {
			reportBrowserFailures(stderr, result)
		}
		return exitFailure
	}
	if !invocation.JSON {
		fmt.Fprintf(
			stdout,
			"codeflux-dev test-browser: passed %d checks against %s\n",
			len(result.Checks),
			result.BaseURL,
		)
	}
	return exitSuccess
}

func writeBrowserUnavailable(
	stdout io.Writer,
	stderr io.Writer,
	jsonOutput bool,
	reason string,
) int {
	if jsonOutput {
		result := struct {
			SchemaVersion int    `json:"schema_version"`
			Command       string `json:"command"`
			Status        string `json:"status"`
			Reason        string `json:"reason"`
		}{
			SchemaVersion: 1,
			Command:       "test-browser",
			Status:        "unavailable",
			Reason:        reason,
		}
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintf(stderr, "codeflux-dev test-browser: encode unavailable result: %v\n", err)
			return exitFailure
		}
		return exitUnavailable
	}
	fmt.Fprintf(stderr, "codeflux-dev test-browser: unavailable: %s\n", reason)
	return exitUnavailable
}

func reportBrowserFailures(output io.Writer, result frontendtest.Result) {
	fmt.Fprintf(output, "codeflux-dev test-browser: %s\n", result.Status)
	for _, check := range result.Checks {
		if check.Passed {
			continue
		}
		fmt.Fprintf(
			output,
			"  %s route=%s viewport=%s: %s",
			check.ID,
			check.Route,
			check.Viewport,
			check.Detail,
		)
		if check.Artifact != "" {
			fmt.Fprintf(output, " artifact=%s", check.Artifact)
		}
		fmt.Fprintln(output)
	}
}
