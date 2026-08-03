package executor

import (
	"strings"
	"testing"
)

// TestNonSourceIsRefusedByShapeNotByColumnNumber covers what rung 6 sent.
//
// "<<", "*", "noop" and "#" arrived as Go source across two attempts, and the
// parser answered each with a column number. The run needs the category, not
// the symptom: what arrived is not source at all.
func TestNonSourceIsRefusedByShapeNotByColumnNumber(t *testing.T) {
	for _, notSource := range []string{
		"<<", "*", "noop", "#", "```go\npackage main\n```",
		"--- a/main.go\n+++ b/main.go", "Here is the file:\npackage main",
	} {
		err := goSourceIsWellFormed("cmd/generated/main.go", notSource)
		if err == nil {
			t.Errorf("%q was accepted as Go source", notSource)
			continue
		}
		if !strings.Contains(err.Error(), "begins with a package clause") {
			t.Errorf("%q was refused with a parser message rather than by "+
				"shape: %v", notSource, err)
		}
	}
}

// TestRealSourceIsNotRefusedByTheShapeCheck keeps the cheap check from
// rejecting the files it exists to let through.
func TestRealSourceIsNotRefusedByTheShapeCheck(t *testing.T) {
	for _, source := range []string{
		"package main\n",
		"// Package main does a thing.\npackage main\n",
		"\n\n// leading blank lines\npackage main\n",
		"/* a block comment first */\npackage main\n",
	} {
		if err := goSourceIsWellFormed("main.go", source); err != nil {
			t.Errorf("valid source was refused: %v\n%s", err, source)
		}
	}
}
