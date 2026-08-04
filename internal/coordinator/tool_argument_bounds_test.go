package coordinator

import (
	"testing"

	"codeflux.dev/codeflux/internal/executor"
)

// TestAWriteAndAPatchMayCarryAWholeFile is the bound that cost an attempt.
//
// Both arguments inherited the loop's four-kilobyte default for an unbounded
// argument, which is generous for a path and far too small for the thing being
// written: a produced test file passes four kilobytes as soon as it has a table
// in it, and so does a patch that adds a few cases to one.
//
// The cost is not a refused call the run could answer. The bound is checked
// while the turn is decoded, so the whole turn is malformed and the attempt is
// spent. Ladder rung 5 on 2026-08-03 lost attempt 5 to "model tool argument
// \"patch\" exceeds its bound" and then ran out of attempts one uncovered line
// short of passing.
func TestAWriteAndAPatchMayCarryAWholeFile(t *testing.T) {
	bounds := map[executor.ToolName]map[string]int{}
	for _, tool := range agentApprovedTools(true) {
		byName := map[string]int{}
		for _, argument := range tool.Arguments {
			byName[argument.Name] = argument.MaxBytes
		}
		bounds[tool.Descriptor.Name] = byName
	}

	for _, expected := range []struct {
		tool     executor.ToolName
		argument string
	}{
		{executor.ToolApplyEdit, "content"},
		{executor.ToolApplyPatch, "patch"},
	} {
		got := bounds[expected.tool][expected.argument]
		if got < 16<<10 {
			t.Errorf("%s's %q may carry only %d bytes; a produced test file is "+
				"routinely larger, and exceeding it costs the whole attempt",
				expected.tool, expected.argument, got)
		}
		// Half the loop's own ceiling for a whole tool call, so one argument
		// cannot crowd out the rest of it.
		if got > 32<<10 {
			t.Errorf("%s's %q may carry %d bytes, which leaves too little of "+
				"the call for everything else", expected.tool, expected.argument, got)
		}
	}
	// The path is not the payload and does not need the same room.
	if got := bounds[executor.ToolApplyPatch]["path"]; got != 0 {
		t.Errorf("the patch path declares a bound of %d; it is a path, and the "+
			"loop's default is already generous for one", got)
	}
}
