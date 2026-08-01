// Package testfixtures is the shared test harness for Milestone 22.
//
// It exists so every suite draws on ONE definition of determinism, one fake
// provider, and one repository-fixture builder. Before this, each package
// grew its own, which made "the fixtures disagree" a plausible explanation
// for a failure — the worst possible property in a harness whose job is
// telling a working prototype from a persuasive demo (docs/plan.md §22 goal).
//
// Nothing here reaches the network, a real provider, or a real credential.
// FixtureCredentialMaterial is the only credential-shaped constant in the
// package and is deliberately non-functional.
package testfixtures

import (
	"fmt"
	"sort"
	"strings"
)

// Suite names one tier of the test pyramid (M22-001).
//
// The tiers are ordered by cost and by how much of the system they bind.
// A defect should be caught by the cheapest tier that can see it; a defect
// only the most expensive tier can see is a gap in the cheaper ones.
type Suite string

const (
	// SuiteFastUnit is pure, in-process, no database, no filesystem, no
	// subprocess. Runs on every change.
	SuiteFastUnit Suite = "fast-unit"
	// SuiteSQLiteIntegration uses a real temporary SQLite database. Proves
	// constraints, migrations, transactions, and recovery.
	SuiteSQLiteIntegration Suite = "sqlite-integration"
	// SuiteProcessIntegration spawns real processes: workers, git, tools.
	SuiteProcessIntegration Suite = "process-integration"
	// SuiteBrowserComponent renders real components in a real browser.
	SuiteBrowserComponent Suite = "browser-component"
	// SuiteEndToEnd drives a whole user journey through the booted
	// application.
	SuiteEndToEnd Suite = "end-to-end"
)

// AllSuites returns every declared tier, cheapest first.
func AllSuites() []Suite {
	return []Suite{
		SuiteFastUnit,
		SuiteSQLiteIntegration,
		SuiteProcessIntegration,
		SuiteBrowserComponent,
		SuiteEndToEnd,
	}
}

// BuildTag is the Go build tag that selects a suite (M22-002).
//
// SuiteFastUnit has no tag on purpose: the cheapest tier must run by default,
// so a contributor who knows nothing about this harness still runs it.
func (suite Suite) BuildTag() string {
	switch suite {
	case SuiteFastUnit:
		return ""
	case SuiteSQLiteIntegration:
		return "sqlite_integration"
	case SuiteProcessIntegration:
		return "process_integration"
	case SuiteBrowserComponent:
		return "browser_component"
	case SuiteEndToEnd:
		return "end_to_end"
	default:
		return ""
	}
}

// TestNamePrefix is the required prefix for a test in this suite (M22-002).
// A reader seeing a failure name should know immediately what it cost to run
// and what it binds.
func (suite Suite) TestNamePrefix() string {
	switch suite {
	case SuiteFastUnit:
		return "Test"
	case SuiteSQLiteIntegration:
		return "TestSQLite"
	case SuiteProcessIntegration:
		return "TestProcess"
	case SuiteBrowserComponent:
		return "TestBrowser"
	case SuiteEndToEnd:
		return "TestEndToEnd"
	default:
		return "Test"
	}
}

// RequiresIsolation reports whether a suite must not share mutable state with
// another suite running concurrently.
func (suite Suite) RequiresIsolation() bool {
	return suite != SuiteFastUnit
}

// Valid reports whether the suite is one of the declared tiers.
func (suite Suite) Valid() bool {
	for _, candidate := range AllSuites() {
		if candidate == suite {
			return true
		}
	}
	return false
}

// ValidateSuiteDefinitions checks the pyramid is internally coherent: every
// tier declared, every name and tag distinct, and exactly one default tier.
//
// This runs as a test rather than living only in prose, so the pyramid
// cannot quietly develop two "default" tiers or two suites that select the
// same build tag.
func ValidateSuiteDefinitions() error {
	suites := AllSuites()
	if len(suites) != 5 {
		return fmt.Errorf("expected 5 declared suites, found %d", len(suites))
	}
	tags := map[string]Suite{}
	prefixes := map[string]Suite{}
	untagged := 0
	for _, suite := range suites {
		if !suite.Valid() {
			return fmt.Errorf("suite %q is not recognised by Valid", suite)
		}
		tag := suite.BuildTag()
		if tag == "" {
			untagged++
		} else {
			if other, clash := tags[tag]; clash {
				return fmt.Errorf("suites %q and %q share build tag %q", other, suite, tag)
			}
			tags[tag] = suite
		}
		prefix := suite.TestNamePrefix()
		if other, clash := prefixes[prefix]; clash && prefix != "Test" {
			return fmt.Errorf("suites %q and %q share test prefix %q", other, suite, prefix)
		}
		prefixes[prefix] = suite
	}
	if untagged != 1 {
		return fmt.Errorf("exactly one suite must run untagged by default, found %d", untagged)
	}
	return nil
}

// SuiteSelectionCommand returns the command that runs one suite, so the
// harness documents itself in one place instead of in scattered comments.
func (suite Suite) SelectionCommand(packages string) string {
	if packages == "" {
		packages = "./..."
	}
	tag := suite.BuildTag()
	if tag == "" {
		return "go test " + packages
	}
	return "go test -tags " + tag + " " + packages
}

// SuiteSummary renders the pyramid for diagnostics output.
func SuiteSummary() string {
	rows := make([]string, 0, len(AllSuites()))
	for _, suite := range AllSuites() {
		tag := suite.BuildTag()
		if tag == "" {
			tag = "(default, untagged)"
		}
		rows = append(rows, fmt.Sprintf("%-20s %-24s %s", suite, tag, suite.TestNamePrefix()))
	}
	sort.SliceStable(rows, func(int, int) bool { return false })
	return strings.Join(rows, "\n")
}
