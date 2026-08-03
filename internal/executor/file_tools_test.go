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
	// Diffs and patch envelopes are refused too, with their own message, which
	// TestAPatchIsRefusedAsAPatch covers. What this covers is everything else
	// that is not source: fences, bare operators, placeholders, prose.
	for _, notSource := range []string{
		"<<", "*", "noop", "#", "```go\npackage main\n```",
		"Here is the file:\npackage main",
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

// TestAPatchIsRefusedAsAPatch covers what rung 6 sent four times.
//
// "Not source at all" does not tell a model which of its habits to drop. The
// tool's own summary read "Apply an expected-hash edit batch", which is a patch
// API described in words, and the model sent patches.
func TestAPatchIsRefusedAsAPatch(t *testing.T) {
	for _, patch := range []string{
		"*** Begin Patch\n*** Update File: main.go\n@@\n-old\n+new\n",
		"@@ -1,4 +1,6 @@\n package main\n",
		"--- a/main.go\n+++ b/main.go\n",
		"diff --git a/main.go b/main.go\n",
	} {
		err := goSourceIsWellFormed("cmd/generated/main.go", patch)
		if err == nil {
			t.Errorf("a patch was accepted as a file: %q", patch)
			continue
		}
		if !strings.Contains(err.Error(), "no patch mode") {
			t.Errorf("a patch was refused without being named as one: %v", err)
		}
	}
}

// TestTheToolSaysWhatItTakes is the other half. A model reads the summary
// before it decides what to send.
func TestTheToolSaysWhatItTakes(t *testing.T) {
	for _, descriptor := range ToolCatalog() {
		if descriptor.Name != ToolApplyEdit {
			continue
		}
		summary := strings.ToLower(descriptor.Summary)
		if !strings.Contains(summary, "complete") {
			t.Errorf("the write tool's summary does not say it takes the "+
				"whole file: %q", descriptor.Summary)
		}
		if !strings.Contains(summary, "not a patch") {
			t.Errorf("the write tool's summary does not rule out a patch: %q",
				descriptor.Summary)
		}
		return
	}
	t.Fatal("the catalog declares no write tool")
}
