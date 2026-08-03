package coordinator

import (
	"strings"
	"testing"
)

// TestMainIsNotAskedToReturnAnError covers a demand no Go program can meet.
//
// Ladder rung 3 was told twice per attempt that main swallowed an error by
// returning none of its own, could not comply, was sent back three times for
// the same finding, escalated to the most expensive rung on the resulting
// stall, and spent the rest of its budget there.
func TestMainIsNotAskedToReturnAnError(t *testing.T) {
	functions := []producedFunction{
		{Name: "run", Parameters: []string{"[]string"},
			Results: []string{"error"}, ReturnsError: true},
		{Name: "main", Calls: []string{"run"},
			Effects: []string{"fmt.Fprintln", "os.Exit"}},
	}
	for _, finding := range findUnhandledFailures(functions) {
		if finding.Where == "main" &&
			strings.Contains(finding.What, "returns no error of its own") {
			t.Errorf("main was asked for a return Go does not allow: %s",
				finding.What)
		}
	}
}

// TestMainMustReportAndExitNonZero is the obligation main genuinely has, and
// the one it can actually satisfy.
func TestMainMustReportAndExitNonZero(t *testing.T) {
	silent := []producedFunction{
		{Name: "run", Results: []string{"error"}, ReturnsError: true},
		{Name: "main", Calls: []string{"run"}, Effects: []string{"fmt.Fprintln"}},
	}
	found := false
	for _, finding := range findUnhandledFailures(silent) {
		if finding.Where == "main" && strings.Contains(finding.What, "os.Exit(1)") {
			found = true
		}
	}
	if !found {
		t.Error("a main that exits zero after a failure was not reported")
	}

	exiting := []producedFunction{
		{Name: "run", Results: []string{"error"}, ReturnsError: true},
		{Name: "main", Calls: []string{"run"},
			Effects: []string{"fmt.Fprintln", "os.Exit"}},
	}
	for _, finding := range findUnhandledFailures(exiting) {
		if finding.Where == "main" {
			t.Errorf("a main that already exits non-zero was still faulted: %s",
				finding.What)
		}
	}
}

// TestAMainThatCannotFailOwesNothing keeps the rule from becoming the same
// unfollowable instruction in a different sentence.
func TestAMainThatCannotFailOwesNothing(t *testing.T) {
	functions := []producedFunction{
		{Name: "greet", Results: []string{"string"}, Pure: true},
		{Name: "main", Calls: []string{"greet"}, Effects: []string{"fmt.Println"}},
	}
	for _, finding := range findUnhandledFailures(functions) {
		if finding.Where == "main" {
			t.Errorf("a main calling nothing fallible was faulted: %s", finding.What)
		}
	}
}
