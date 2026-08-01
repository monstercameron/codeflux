package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/web/frontend/diffreview"
)

// errReviewResourceMalformed and reviewHunkHeaderPattern back
// parseReviewUnifiedDiff, which is exercised natively by
// review_resource_test.go. They stay in the untagged file; the
// bridge/scope sentinels live with the wasm-only load path.
var (
	errReviewResourceMalformed = errors.New("authoritative review resource is malformed")
	reviewHunkHeaderPattern    = regexp.MustCompile(`^@@ -([0-9]+)(?:,[0-9]+)? \+([0-9]+)(?:,[0-9]+)? @@`)
)

type reviewFileLinks struct {
	steps       []taskgraph.PlanStepLink
	events      []domain.EventID
	validations []diffreview.ValidationLink
}

func parseReviewUnifiedDiff(
	value string,
	files []diffreview.ChangedFile,
	links map[string]reviewFileLinks,
) (map[string][]diffreview.DiffHunk, error) {
	known := make(map[string]string, len(files)*2)
	for _, file := range files {
		known[file.Path.String()] = file.Path.String()
		if !file.PreviousPath.IsZero() {
			known[file.PreviousPath.String()] = file.Path.String()
		}
	}
	result := make(map[string][]diffreview.DiffHunk, len(files))
	currentPath := ""
	var current *diffreview.DiffHunk
	var oldLine, newLine uint64
	flush := func() {
		if current != nil && currentPath != "" {
			result[currentPath] = append(result[currentPath], *current)
		}
		current = nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			currentPath = ""
			continue
		}
		if strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- ") {
			candidate := strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "--- ")
			candidate = strings.SplitN(candidate, "\t", 2)[0]
			candidate = strings.TrimPrefix(strings.TrimPrefix(candidate, "a/"), "b/")
			if mapped, ok := known[candidate]; ok {
				currentPath = mapped
			}
			continue
		}
		if match := reviewHunkHeaderPattern.FindStringSubmatch(line); match != nil {
			flush()
			if currentPath == "" {
				return nil, fmt.Errorf("%w: hunk has no known file", errReviewResourceMalformed)
			}
			oldLine, _ = strconv.ParseUint(match[1], 10, 64)
			newLine, _ = strconv.ParseUint(match[2], 10, 64)
			attribution := links[currentPath]
			current = &diffreview.DiffHunk{
				ID: fmt.Sprintf("%s-hunk-%d", currentPath, len(result[currentPath])+1), Header: line,
				PlanSteps: attribution.steps, ToolEventIDs: attribution.events, Validations: attribution.validations,
			}
			continue
		}
		if current == nil || line == `\ No newline at end of file` {
			continue
		}
		if line == "" {
			continue
		}
		var parsed diffreview.DiffLine
		switch line[0] {
		case ' ':
			parsed = diffreview.DiffLine{Kind: diffreview.DiffLineContext, Text: line[1:], OldLineNumberKnown: true, OldLineNumber: oldLine, NewLineNumberKnown: true, NewLineNumber: newLine}
			oldLine++
			newLine++
		case '+':
			parsed = diffreview.DiffLine{Kind: diffreview.DiffLineAddition, Text: line[1:], NewLineNumberKnown: true, NewLineNumber: newLine}
			newLine++
		case '-':
			parsed = diffreview.DiffLine{Kind: diffreview.DiffLineDeletion, Text: line[1:], OldLineNumberKnown: true, OldLineNumber: oldLine}
			oldLine++
		default:
			continue
		}
		current.Lines = append(current.Lines, parsed)
		if len(current.Lines) > diffreview.MaximumLinesPerHunk {
			return nil, fmt.Errorf("%w: hunk exceeds line bound", errReviewResourceMalformed)
		}
	}
	flush()
	return result, nil
}
