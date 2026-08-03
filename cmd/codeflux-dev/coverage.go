package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// coverageMinimumEnvironmentVariable is the environment variable
// .githooks/pre-push already reads to override its built-in floor for a
// single run. test-coverage honors the same variable so a developer or CI
// job can set one knob and have both the hook and the command agree.
const coverageMinimumEnvironmentVariable = "CODEFLUX_MIN_COVERAGE"

// enforceCoverageFloor resolves the requested coverage threshold, if any,
// and — only once the suite has already passed and a profile exists at
// profilePath — measures statement coverage and refuses when it falls below
// the floor. Called with no threshold requested (flagValue and the
// CODEFLUX_MIN_COVERAGE environment variable both empty), it leaves the
// existing test-coverage behavior unchanged: the profile is written and
// nothing is asserted.
func enforceCoverageFloor(
	ctx context.Context,
	root string,
	stdout io.Writer,
	stderr io.Writer,
	profilePath string,
	flagValue string,
) int {
	threshold, enabled, source, err := resolveCoverageThreshold(flagValue, os.Getenv(coverageMinimumEnvironmentVariable))
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev test-coverage: %v\n", err)
		return exitUsage
	}
	if !enabled {
		return exitSuccess
	}

	output, err := runGoToolCoverFunc(ctx, root, profilePath)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev test-coverage: %v\n", err)
		return exitFailure
	}
	total, err := parseCoverageTotalPercent(output)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev test-coverage: %v\n", err)
		return exitFailure
	}

	if !meetsCoverageFloor(total, threshold) {
		fmt.Fprintf(
			stderr,
			"codeflux-dev test-coverage: statement coverage is %.1f%%, below the %.1f%% floor (from %s).\n",
			total, threshold, source,
		)
		fmt.Fprintln(stderr, "codeflux-dev test-coverage: lowest-covered functions:")
		for _, line := range lowestCoveredFunctionLines(output, 15) {
			fmt.Fprintf(stderr, "  %s\n", line)
		}
		return exitFailure
	}
	fmt.Fprintf(stdout, "codeflux-dev test-coverage: statement coverage %.1f%% (floor %.1f%%, from %s)\n", total, threshold, source)
	return exitSuccess
}

// resolveCoverageThreshold decides which of --min-coverage and
// CODEFLUX_MIN_COVERAGE governs this run. The flag wins when both are set:
// it is the explicit, per-invocation signal, while the environment variable
// exists for ambient overrides such as .githooks/pre-push's own default and
// bisecting. Neither set means no floor is enforced, matching
// test-coverage's behavior before --min-coverage existed.
func resolveCoverageThreshold(flagValue, envValue string) (threshold float64, enabled bool, source string, err error) {
	if strings.TrimSpace(flagValue) != "" {
		value, parseErr := parseCoveragePercent(flagValue)
		if parseErr != nil {
			return 0, false, "", fmt.Errorf("--min-coverage: %w", parseErr)
		}
		return value, true, "--min-coverage", nil
	}
	if strings.TrimSpace(envValue) != "" {
		value, parseErr := parseCoveragePercent(envValue)
		if parseErr != nil {
			return 0, false, "", fmt.Errorf("%s: %w", coverageMinimumEnvironmentVariable, parseErr)
		}
		return value, true, coverageMinimumEnvironmentVariable, nil
	}
	return 0, false, "", nil
}

// parseCoveragePercent parses a floor expressed as a bare number ("80") or a
// number with a trailing percent sign ("80%"), both of which
// CODEFLUX_MIN_COVERAGE and --min-coverage accept.
func parseCoveragePercent(raw string) (float64, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(raw), "%")
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number: %w", raw, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("%q must not be negative", raw)
	}
	return value, nil
}

// meetsCoverageFloor reports whether total statement coverage satisfies
// threshold. Equal to the floor passes, matching AGENTS.md's "at or above".
func meetsCoverageFloor(total, threshold float64) bool {
	return total >= threshold
}

// parseCoverageTotalPercent extracts the percentage from the "total:" line
// `go tool cover -func` emits, e.g. "total:\t\t\t(statements)\t50.0%".
func parseCoverageTotalPercent(coverFuncOutput string) (float64, error) {
	for _, line := range strings.Split(coverFuncOutput, "\n") {
		if !strings.HasPrefix(line, "total:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			break
		}
		last := fields[len(fields)-1]
		value, err := strconv.ParseFloat(strings.TrimSuffix(last, "%"), 64)
		if err != nil {
			return 0, fmt.Errorf("parse total coverage %q: %w", last, err)
		}
		return value, nil
	}
	return 0, fmt.Errorf("no total line in coverage output")
}

// lowestCoveredFunctionLines returns up to limit non-total lines from `go
// tool cover -func` output, sorted ascending by their own percentage, so the
// worst-covered functions are named for whoever has to close the gap. This
// mirrors the diagnostic .githooks/pre-push printed inline before it began
// delegating measurement to this command.
func lowestCoveredFunctionLines(coverFuncOutput string, limit int) []string {
	type scoredLine struct {
		line    string
		percent float64
	}
	var scored []scoredLine
	for _, line := range strings.Split(coverFuncOutput, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "total:") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		percent, err := strconv.ParseFloat(strings.TrimSuffix(fields[len(fields)-1], "%"), 64)
		if err != nil {
			continue
		}
		scored = append(scored, scoredLine{line: trimmed, percent: percent})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].percent < scored[j].percent
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	lines := make([]string, 0, len(scored))
	for _, entry := range scored {
		lines = append(lines, entry.line)
	}
	return lines
}

// runGoToolCoverFunc runs `go tool cover -func` against an already-written
// coverage profile and returns its combined output.
func runGoToolCoverFunc(ctx context.Context, root, profilePath string) (string, error) {
	command := exec.CommandContext(ctx, "go", "tool", "cover", "-func="+profilePath)
	command.Dir = root
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return output.String(), fmt.Errorf("go tool cover -func: %w: %s", err, output.String())
	}
	return output.String(), nil
}
