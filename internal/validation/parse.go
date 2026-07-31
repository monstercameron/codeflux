package validation

import (
	"bufio"
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type ParsedOutput struct {
	ParserName     string
	ParseSucceeded bool
	JSON           string
	RawSummary     string
}

type goTestEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
}

type goTestParsed struct {
	Packages []string `json:"packages"`
	Tests    []string `json:"tests"`
}

type formatterParsed struct {
	ChangedFiles []string `json:"changed_files"`
}

type diagnostic struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column,omitempty"`
	Message string `json:"message"`
}

type diagnosticsParsed struct {
	Diagnostics []diagnostic `json:"diagnostics"`
}

var diagnosticPattern = regexp.MustCompile(`^(.+\.go):(\d+)(?::(\d+))?:\s*(.+)$`)

// ParseCheckOutput recognizes bounded Go validation output. The raw redacted
// summary is always retained, including when no parser recognizes the output.
func ParseCheckOutput(check Check, stdout, stderr, executionSummary string) ParsedOutput {
	raw := rawRedactedSummary(stdout, stderr, executionSummary)
	name := "raw-redacted-v1"
	parsed := any(struct{}{})
	succeeded := false
	switch check.Class {
	case CheckTargetedTest, CheckBroadTest:
		name = "go-test-v1"
		parsed, succeeded = parseGoTest(stdout + "\n" + stderr)
	case CheckFormatter:
		name = "go-formatter-v1"
		parsed, succeeded = parseFormatter(stdout + "\n" + stderr)
	case CheckBuild, CheckStaticAnalysis:
		name = "go-diagnostics-v1"
		parsed, succeeded = parseDiagnostics(stdout + "\n" + stderr)
	}
	encoded, err := json.Marshal(parsed)
	if err != nil || len(encoded) > MaximumParsedResultBytes {
		encoded = []byte(`{}`)
		succeeded = false
		name = "raw-redacted-v1"
	}
	return ParsedOutput{ParserName: name, ParseSucceeded: succeeded, JSON: string(encoded), RawSummary: raw}
}

func parseGoTest(output string) (goTestParsed, bool) {
	packages := make(map[string]struct{})
	tests := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 4096), MaximumCapturedOutputBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		var event goTestEvent
		if json.Unmarshal([]byte(line), &event) == nil && event.Action != "" {
			if boundedParsedName(event.Package) {
				packages[event.Package] = struct{}{}
			}
			if boundedParsedName(event.Test) {
				tests[event.Test] = struct{}{}
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[0] == "ok" || fields[0] == "FAIL") && boundedParsedName(fields[1]) {
			packages[fields[1]] = struct{}{}
		}
		if len(fields) >= 3 && fields[0] == "---" && strings.HasSuffix(fields[1], ":") && boundedParsedName(fields[2]) {
			tests[fields[2]] = struct{}{}
		}
	}
	result := goTestParsed{Packages: sortedKeys(packages), Tests: sortedKeys(tests)}
	return result, len(result.Packages) != 0 || len(result.Tests) != 0
}

func parseFormatter(output string) (formatterParsed, bool) {
	seen := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		candidate := strings.TrimSpace(line)
		if candidate == "" || len(candidate) > 4096 || !strings.HasSuffix(strings.ToLower(candidate), ".go") {
			continue
		}
		candidate = filepath.ToSlash(filepath.Clean(candidate))
		seen[candidate] = struct{}{}
	}
	result := formatterParsed{ChangedFiles: sortedKeys(seen)}
	return result, len(result.ChangedFiles) != 0
}

func parseDiagnostics(output string) (diagnosticsParsed, bool) {
	result := diagnosticsParsed{}
	for _, line := range strings.Split(output, "\n") {
		if len(result.Diagnostics) == 128 {
			break
		}
		match := diagnosticPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) != 5 || len(match[1]) > 4096 || len(match[4]) > 4096 {
			continue
		}
		lineNumber, lineErr := strconv.Atoi(match[2])
		column := 0
		var columnErr error
		if match[3] != "" {
			column, columnErr = strconv.Atoi(match[3])
		}
		if lineErr != nil || columnErr != nil || lineNumber < 1 || column < 0 {
			continue
		}
		result.Diagnostics = append(result.Diagnostics, diagnostic{
			File: filepath.ToSlash(match[1]), Line: lineNumber, Column: column,
			Message: match[4],
		})
	}
	return result, len(result.Diagnostics) != 0
}

func rawRedactedSummary(stdout, stderr, fallback string) string {
	parts := make([]string, 0, 2)
	if value := strings.TrimSpace(stdout); value != "" {
		parts = append(parts, "stdout:\n"+value)
	}
	if value := strings.TrimSpace(stderr); value != "" {
		parts = append(parts, "stderr:\n"+value)
	}
	result := strings.Join(parts, "\n")
	if result == "" {
		result = strings.TrimSpace(fallback)
	}
	if result == "" {
		result = "validation command produced no output"
	}
	if len(result) > MaximumCapturedOutputBytes {
		result = result[:MaximumCapturedOutputBytes]
	}
	return strings.TrimSpace(result)
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func boundedParsedName(value string) bool {
	return strings.TrimSpace(value) == value && value != "" && len(value) <= 4096
}
