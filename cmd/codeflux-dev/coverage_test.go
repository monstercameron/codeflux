package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Pure helper tests -----------------------------------------------------

func TestResolveCoverageThreshold_NeitherSet_Disabled(t *testing.T) {
	threshold, enabled, source, err := resolveCoverageThreshold("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Fatalf("threshold enabled with no flag and no env; threshold=%v source=%q", threshold, source)
	}
}

func TestResolveCoverageThreshold_FlagOnly(t *testing.T) {
	threshold, enabled, source, err := resolveCoverageThreshold("75", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled || threshold != 75 || source != "--min-coverage" {
		t.Fatalf("got enabled=%v threshold=%v source=%q, want enabled=true threshold=75 source=--min-coverage", enabled, threshold, source)
	}
}

func TestResolveCoverageThreshold_EnvOnly(t *testing.T) {
	threshold, enabled, source, err := resolveCoverageThreshold("", "65")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled || threshold != 65 || source != coverageMinimumEnvironmentVariable {
		t.Fatalf("got enabled=%v threshold=%v source=%q, want enabled=true threshold=65 source=%s", enabled, threshold, source, coverageMinimumEnvironmentVariable)
	}
}

// TestResolveCoverageThreshold_FlagWinsOverEnv is the discriminating case for
// "make the flag and the variable agree, saying which wins": with both set to
// different values, the flag's value must be the one that governs, not the
// environment variable's.
func TestResolveCoverageThreshold_FlagWinsOverEnv(t *testing.T) {
	threshold, enabled, source, err := resolveCoverageThreshold("90", "10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled || threshold != 90 || source != "--min-coverage" {
		t.Fatalf("got enabled=%v threshold=%v source=%q, want the flag (90) to win over the env var (10)", enabled, threshold, source)
	}
}

func TestResolveCoverageThreshold_InvalidFlagRejected(t *testing.T) {
	if _, _, _, err := resolveCoverageThreshold("not-a-number", ""); err == nil {
		t.Fatal("non-numeric --min-coverage was accepted")
	}
}

func TestResolveCoverageThreshold_NegativeFlagRejected(t *testing.T) {
	if _, _, _, err := resolveCoverageThreshold("-5", ""); err == nil {
		t.Fatal("negative --min-coverage was accepted")
	}
}

func TestResolveCoverageThreshold_InvalidEnvRejected(t *testing.T) {
	if _, _, _, err := resolveCoverageThreshold("", "garbage"); err == nil {
		t.Fatal("non-numeric CODEFLUX_MIN_COVERAGE was accepted")
	}
}

func TestParseCoveragePercent_AcceptsTrailingPercentSign(t *testing.T) {
	value, err := parseCoveragePercent("80%")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != 80 {
		t.Fatalf("got %v, want 80", value)
	}
}

func TestMeetsCoverageFloor_EqualPasses(t *testing.T) {
	if !meetsCoverageFloor(80, 80) {
		t.Fatal("coverage equal to the floor was reported as failing; AGENTS.md says \"at or above\"")
	}
}

// TestMeetsCoverageFloor_BelowFails is the discriminating negative case: a
// check that always returns true regardless of input would pass every other
// test in this file but must fail this one.
func TestMeetsCoverageFloor_BelowFails(t *testing.T) {
	if meetsCoverageFloor(79.9, 80) {
		t.Fatal("coverage below the floor was reported as passing")
	}
}

func TestParseCoverageTotalPercent_ParsesTotalLine(t *testing.T) {
	output := "codeflux.dev/x/y.go:12:\tFoo\t100.0%\ntotal:\t\t\t(statements)\t63.4%\n"
	total, err := parseCoverageTotalPercent(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 63.4 {
		t.Fatalf("got %v, want 63.4", total)
	}
}

// TestParseCoverageTotalPercent_MissingTotalLineErrors is the violating-input
// case: output with no "total:" line (e.g. a corrupted or truncated `go tool
// cover -func` run) must be reported as unparseable, not silently treated as
// 0% or 100%.
func TestParseCoverageTotalPercent_MissingTotalLineErrors(t *testing.T) {
	if _, err := parseCoverageTotalPercent("codeflux.dev/x/y.go:12:\tFoo\t100.0%\n"); err == nil {
		t.Fatal("output with no total line was accepted")
	}
}

func TestLowestCoveredFunctionLines_SortsAscendingAndLimits(t *testing.T) {
	output := strings.Join([]string{
		"a.go:1:\tHigh\t90.0%",
		"a.go:2:\tLow\t10.0%",
		"a.go:3:\tMid\t50.0%",
		"total:\t\t\t(statements)\t50.0%",
	}, "\n")
	lines := lowestCoveredFunctionLines(output, 2)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "Low") || !strings.Contains(lines[1], "Mid") {
		t.Fatalf("got %v, want Low then Mid (ascending) with High and total excluded", lines)
	}
}

// --- End-to-end tests against an isolated fixture module -------------------
//
// These drive the real run() dispatcher, including the actual `go test
// -coverprofile` and `go tool cover -func` subprocesses, against a tiny
// throwaway module so they run in a couple of seconds rather than pulling in
// this repository's own multi-minute suite.

// coverageFixtureSource is a package with exactly two one-statement
// functions. A test exercising only Covered leaves statement coverage at
// exactly 50.0%, which is deterministic and lets the threshold tests bracket
// it precisely (40 passes, 60 fails, 50 passes at the boundary).
const coverageFixtureSource = "package sample\n\nfunc Covered() int { return 1 }\n\nfunc Uncovered() int { return 2 }\n"

const coverageFixturePassingTest = "package sample\n\nimport \"testing\"\n\nfunc TestCovered(t *testing.T) {\n\tif Covered() != 1 {\n\t\tt.Fatal(\"unexpected\")\n\t}\n}\n"

const coverageFixtureFailingTest = "package sample\n\nimport \"testing\"\n\nfunc TestCovered(t *testing.T) {\n\tif Covered() != 1 {\n\t\tt.Fatal(\"unexpected\")\n\t}\n}\n\nfunc TestDeliberatelyFails(t *testing.T) {\n\tt.Fatal(\"deliberate failure so the suite is refused, not measured\")\n}\n"

// setUpCoverageFixtureModule creates a standalone Go module with a package at
// exactly 50.0% statement coverage, chdirs the test process into it so
// repositoryRoot() resolves to the fixture rather than this repository, and
// restores the original working directory on cleanup.
func setUpCoverageFixtureModule(t *testing.T, testSource string) string {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module coveragefixture\n\ngo 1.26.0\n")
	writeTestFile(t, filepath.Join(root, "sample.go"), coverageFixtureSource)
	writeTestFile(t, filepath.Join(root, "sample_test.go"), testSource)

	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir into fixture module: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	return root
}

func TestCoverageCommand_BelowFloor_Fails(t *testing.T) {
	setUpCoverageFixtureModule(t, coverageFixturePassingTest)
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{"test-coverage", "--min-coverage", "60"})
	if code == exitSuccess {
		t.Fatalf("50%% coverage against a 60%% floor was accepted; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "below the 60.0% floor") {
		t.Fatalf("stderr = %q, want it to name the floor it fell below", stderr.String())
	}
}

// TestCoverageCommand_AtFloor_Passes is the required non-vacuous positive
// case for the boundary: coverage exactly equal to the floor must pass, per
// AGENTS.md's "at or above".
func TestCoverageCommand_AtFloor_Passes(t *testing.T) {
	setUpCoverageFixtureModule(t, coverageFixturePassingTest)
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{"test-coverage", "--min-coverage", "50"})
	if code != exitSuccess {
		t.Fatalf("50%% coverage against a 50%% floor was refused; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "50.0%") {
		t.Fatalf("stdout = %q, want the measured percentage reported", stdout.String())
	}
}

func TestCoverageCommand_BelowFloor_ViaEnvironmentVariable(t *testing.T) {
	setUpCoverageFixtureModule(t, coverageFixturePassingTest)
	t.Setenv(coverageMinimumEnvironmentVariable, "60")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{"test-coverage"})
	if code == exitSuccess {
		t.Fatalf("CODEFLUX_MIN_COVERAGE=60 against 50%% coverage was accepted; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), coverageMinimumEnvironmentVariable) {
		t.Fatalf("stderr = %q, want it to attribute the floor to %s", stderr.String(), coverageMinimumEnvironmentVariable)
	}
}

// TestCoverageCommand_NoThresholdRequested_Unenforced pins the backward
// compatibility requirement: with neither --min-coverage nor
// CODEFLUX_MIN_COVERAGE set, test-coverage must keep behaving exactly as it
// did before this ticket -- write the profile, assert nothing -- even though
// the fixture's 50% coverage would fail any floor above 50.
func TestCoverageCommand_NoThresholdRequested_Unenforced(t *testing.T) {
	setUpCoverageFixtureModule(t, coverageFixturePassingTest)
	t.Setenv(coverageMinimumEnvironmentVariable, "")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{"test-coverage"})
	if code != exitSuccess {
		t.Fatalf("unthresholded test-coverage failed; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

// TestCoverageCommand_FailingSuite_RefusedNotMeasured is the discriminating
// case for "a failing suite is refused, not measured": even with a floor of 0
// -- which the fixture's real coverage would clear easily -- a failing test
// must still refuse the run rather than reporting a passing percentage.
func TestCoverageCommand_FailingSuite_RefusedNotMeasured(t *testing.T) {
	setUpCoverageFixtureModule(t, coverageFixtureFailingTest)
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{"test-coverage", "--min-coverage", "0"})
	if code == exitSuccess {
		t.Fatalf("a failing suite was reported as a passing coverage run; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "coverage was not measured") {
		t.Fatalf("stderr = %q, want it to say coverage was not measured, not just that the run failed", stderr.String())
	}
	if strings.Contains(stdout.String(), "statement coverage") {
		t.Fatalf("stdout = %q, a failing suite must not print a coverage percentage", stdout.String())
	}
}

// --- Option-guard tests ------------------------------------------------

func TestMinCoverageFlag_RejectedForOtherCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{"lint", "--min-coverage", "80"})
	if code != exitUsage {
		t.Fatalf("got exit %d, want exitUsage for --min-coverage passed to lint; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--min-coverage") {
		t.Fatalf("stderr = %q, want it to name the rejected option", stderr.String())
	}
}

func TestMinCoverageFlag_MissingValueIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{"test-coverage", "--min-coverage"})
	if code != exitUsage {
		t.Fatalf("got exit %d, want exitUsage for --min-coverage with no value; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestMinCoverageFlag_InvalidValueIsUsageError(t *testing.T) {
	setUpCoverageFixtureModule(t, coverageFixturePassingTest)
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{"test-coverage", "--min-coverage", "not-a-number"})
	if code != exitUsage {
		t.Fatalf("got exit %d, want exitUsage for a non-numeric --min-coverage; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
