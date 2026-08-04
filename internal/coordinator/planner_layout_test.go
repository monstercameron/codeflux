package coordinator

import (
	"slices"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
)

// TestThePlannerCanNameMoreThanOnePackage is the layout that could not be
// expressed.
//
// filesNamedIn answered with cmd/generated/main.go and its test whenever a
// request named no path, so how many packages a run could produce was a
// constant. A request whose point is that a command imports a package it
// exports from was planned into one directory, and nothing downstream could
// have recovered: the plan's files are the scope every write is checked
// against, so a second package was unreachable rather than merely unplanned.
//
// Proven to discriminate: ladder rung 16 on 2026-08-04 satisfied thirty-one
// stages with none failing and delivered one package, because every gate checks
// the plan and the plan was already wrong.
func TestThePlannerCanNameMoreThanOnePackage(t *testing.T) {
	files := layoutFromPlanner([]string{
		"internal/stats/stats.go", "cmd/report/main.go",
	})

	directories := map[string]bool{}
	for _, file := range files {
		if cut := strings.LastIndexByte(file, '/'); cut >= 0 {
			directories[file[:cut]] = true
		}
	}
	if len(directories) != 2 {
		t.Fatalf("a two-package layout became %d directory: %v",
			len(directories), files)
	}

	// A test beside every source file, because the gates ask each function for
	// a direct test and a run asked for one with nowhere to write it is refused
	// for the plan's omission rather than its own.
	for _, source := range []string{
		"internal/stats/stats.go", "cmd/report/main.go",
	} {
		test := strings.TrimSuffix(source, ".go") + "_test.go"
		if !slices.Contains(files, test) {
			t.Errorf("%s has no test file in the plan: %v", source, files)
		}
	}
}

// TestALayoutWithNothingToRunIsRefused is the first control.
//
// Every request here asks for a program that is built and executed, and the
// ladder runs the binary. A plan of libraries would satisfy its own steps and
// produce nothing to invoke, which reads as a run that did everything asked.
func TestALayoutWithNothingToRunIsRefused(t *testing.T) {
	if files := layoutFromPlanner([]string{
		"internal/stats/stats.go", "internal/parse/parse.go",
	}); files != nil {
		t.Fatalf("a layout with no command was accepted: %v", files)
	}
	// A main.go at the module root counts, and so does anything under cmd/.
	if files := layoutFromPlanner([]string{"main.go"}); len(files) == 0 {
		t.Error("a root main.go is a command")
	}
	if files := layoutFromPlanner([]string{"cmd/x/main.go"}); len(files) == 0 {
		t.Error("a cmd directory is a command")
	}
}

// TestAnUnusableLayoutIsDiscardedWhole is the second control, and it is what
// makes the fallback safe.
//
// The plan's file list is the scope every write is checked against, so a layout
// is not a suggestion a run can partly follow. Repairing a bad path would mean
// inventing the one the planner did not give; keeping the good paths and
// dropping the bad one would silently narrow what the run may write. Falling
// back is a worse layout and a working run, which is the trade planning already
// makes when the planner does not answer at all.
func TestAnUnusableLayoutIsDiscardedWhole(t *testing.T) {
	for _, unusable := range [][]string{
		{"cmd/x/main.go", "../outside/thing.go"},
		{"cmd/x/main.go", "/etc/passwd"},
		{"cmd/x/main.go", "C:\\windows\\thing.go"},
		{"cmd/x/main.go", "docs/README.md"},
		{"cmd/x/main.go", ""},
	} {
		if files := layoutFromPlanner(unusable); files != nil {
			t.Errorf("%v was accepted as %v", unusable, files)
		}
	}
}

// TestALayoutIsBounded keeps a plan from becoming a project.
//
// Every file in the plan has to be written, tested and documented inside one
// attempt budget. A layout naming more than a program's worth is a
// decomposition problem, and accepting it would spend the whole budget
// discovering that.
func TestALayoutIsBounded(t *testing.T) {
	var many []string
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		many = append(many, "internal/"+name+"/"+name+".go")
	}
	many = append(many, "cmd/x/main.go")
	if files := layoutFromPlanner(many); files != nil {
		t.Errorf("a layout of %d sources became a plan of %d files",
			len(many), len(files))
	}
}

// TestNoLayoutNamedLeavesThePlanAlone is what a request that already says where
// the work goes relies on.
//
// A path in the request is the person saying where the work goes and outranks
// anything the planner would choose. The schema requires the files field, so
// "no layout" arrives as an empty list rather than an absent one, and it has to
// read as "keep what you had" rather than as "put it nowhere".
func TestNoLayoutNamedLeavesThePlanAlone(t *testing.T) {
	if files := layoutFromPlanner(nil); files != nil {
		t.Errorf("an absent layout produced %v", files)
	}
	if files := layoutFromPlanner([]string{}); files != nil {
		t.Errorf("an empty layout produced %v", files)
	}
}

// TestARequestThatNamesAFileKeepsIt pins the precedence.
//
// requirementNamesNoFile is what gates the planner's layout, and it reads the
// same extractor filesNamedIn does. Two readers of one requirement drifting is
// how a plan came to name a file no step covered.
func TestARequestThatNamesAFileKeepsIt(t *testing.T) {
	if requirementNamesNoFile("Write greet.go so that Greet prints one line.") {
		t.Error("a request naming greet.go was read as leaving the layout open")
	}
	if !requirementNamesNoFile(
		"Write a program that reads its arguments as integers and prints the sum.",
	) {
		t.Error("a request naming no path was read as having given a layout")
	}
}

// TestThePlanTheLoopValidatesAcceptsAPlannerLayout is the agreement that has
// already been got wrong once.
//
// The coordinator decides a layout and the loop then decides whether that
// layout is admissible. When those two disagreed about completion tools, every
// plan was refused before the first prompt was sent, three attempts produced
// nothing, and the failure surfaced as a model-quality problem it was not.
func TestThePlanTheLoopValidatesAcceptsAPlannerLayout(t *testing.T) {
	worktree := t.TempDir()
	files := layoutFromPlanner([]string{
		"internal/stats/stats.go", "cmd/report/main.go",
	})
	if len(files) == 0 {
		t.Fatal("the fixture needs a layout")
	}
	steps := agentPlanStepsForFiles(worktree, "Write a program.", files)
	if len(steps) == 0 {
		t.Fatal("a layout produced no steps")
	}
	for _, step := range steps {
		if err := agentloop.ValidatePlanStep(step); err != nil {
			t.Errorf("step %q (kind %s, files %v) is refused by the loop that "+
				"validates it: %v", step.ID, step.Kind, step.ExpectedFiles, err)
		}
	}
}
