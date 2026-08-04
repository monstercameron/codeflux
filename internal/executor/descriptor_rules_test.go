package executor

import (
	"strings"
	"testing"
)

// descriptorFor finds a tool's declared summary.
func descriptorFor(t *testing.T, name ToolName) string {
	t.Helper()
	for _, descriptor := range ToolCatalog() {
		if descriptor.Name == name {
			return descriptor.Summary
		}
	}
	t.Fatalf("no descriptor declares %s", name)
	return ""
}

// TestADescriptorStatesTheRuleBeforeTheRefusal is the same principle the round
// document now follows, applied to the tools.
//
// Three rules of this pipeline were stated only in the refusal that enforced
// them, and each cost rounds before it was ever read. The suite is answered from
// the last run when nothing has changed, and refused after twice — ladder rung
// 16 spent 40 of its 73 rounds asking anyway, writing six times in all. A second
// wholesale rewrite of one file in an attempt is refused — rung 9 hit that seven
// times in a single pass. A patch naming two files is refused, which was added
// to that descriptor earlier and is asserted here so all three stay stated.
//
// A refusal is a correction after the round is spent. The descriptor is the only
// place that can stop the round being spent.
func TestADescriptorStatesTheRuleBeforeTheRefusal(t *testing.T) {
	for _, subject := range []struct {
		tool  ToolName
		rules map[string]string
	}{
		{
			tool: ToolTest,
			rules: map[string]string{
				"the answer is cached when nothing changed": "not changed since the last run",
				"repeating it is refused":                   "refused",
			},
		},
		{
			tool: ToolApplyEdit,
			rules: map[string]string{
				"a second wholesale rewrite is refused": "second wholesale rewrite",
			},
		},
		{
			tool: ToolApplyPatch,
			rules: map[string]string{
				"one file per call":   "One file per call",
				"context is required": "two unprefixed lines",
			},
		},
	} {
		summary := descriptorFor(t, subject.tool)
		for rule, wanted := range subject.rules {
			if !strings.Contains(summary, wanted) {
				t.Errorf("%s does not say %s, so a run learns it from a "+
					"refusal after the round is gone:\n%s",
					subject.tool, rule, summary)
			}
		}
	}
}

// TestTheTestDescriptorSaysHowToCallIt keeps it from being a category.
//
// "Run an approved test recipe" names a category and no action. The arguments
// are not guessable from it, and a run that guesses wrong spends a round
// finding out.
func TestTheTestDescriptorSaysHowToCallIt(t *testing.T) {
	summary := descriptorFor(t, ToolTest)
	for _, wanted := range []string{"go", "test", "./..."} {
		if !strings.Contains(summary, wanted) {
			t.Errorf("the test descriptor never mentions %q, so the call has "+
				"to be guessed:\n%s", wanted, summary)
		}
	}
}
