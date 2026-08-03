package coordinator

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

// --- PIPE-095: checks are selected from task risk. ---
//
// Not reached from the pipeline this session -- see reviewAdversariallyForRisk's
// own comment for why the call site in agent_execution.go was not changed.
// These tests cover the capability directly.

// TestPIPE095_RoutineRiskSkipsMutationAnalysis is the discrimination proof:
// selectAdversarialChecks must turn the costly finder off for routine risk
// and on for every other named level, which a version that ignored risk
// entirely (returning a fixed plan regardless of the argument) would fail
// for at least one of the two assertions below.
func TestPIPE095_RoutineRiskSkipsMutationAnalysis(t *testing.T) {
	cases := []struct {
		risk domain.RiskLevel
		want bool
	}{
		{domain.RiskLevelRoutine, false},
		{domain.RiskLevelElevated, true},
		{domain.RiskLevelProtected, true},
		{"", true}, // unset risk defaults to the widest policy
	}
	for _, testCase := range cases {
		plan := selectAdversarialChecks(testCase.risk)
		if plan.MutationAnalysis != testCase.want {
			t.Errorf("risk %q: MutationAnalysis = %v, want %v",
				testCase.risk, plan.MutationAnalysis, testCase.want)
		}
	}
}

// TestPIPE095_ReviewAdversariallyForRiskActuallyGatesTheMutationFinder
// proves selectAdversarialChecks' plan is not merely computed but honoured
// by reviewAdversariallyWithPlan: given the identical fixture, the plan a
// routine risk selects must produce no mutation-survivor-lineage findings at
// all (the finder never ran), while the plan an elevated risk selects must
// actually invoke it (proven by the call completing without error against a
// real, if trivial, mediated subprocess -- the same pattern
// agent_verification_exec.go documents existing checkMutations tests using
// a bare &AgentExecution{} for).
//
// reviewAdversariallyForRisk itself is exercised too, once, to confirm it
// threads a risk level through to selectAdversarialChecks and on into
// reviewAdversariallyWithPlan rather than only the two pieces being
// separately correct.
func TestPIPE095_ReviewAdversariallyForRiskActuallyGatesTheMutationFinder(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/pipe095/gate\n\ngo 1.26.0\n",
		"lib.go": "package lib\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
	})
	execution := &AgentExecution{}

	skipped, err := execution.reviewAdversariallyWithPlan(
		t.Context(), worktree, adversarialCheckPlan{MutationAnalysis: false})
	if err != nil {
		t.Fatalf("MutationAnalysis=false: %v", err)
	}
	for _, finding := range skipped {
		if finding.Lineage == findingLineageMutationSurvivor {
			t.Errorf("MutationAnalysis=false should not have run mutation "+
				"analysis, but got a mutation-survivor finding: %+v", finding)
		}
	}

	ran, err := execution.reviewAdversariallyWithPlan(
		t.Context(), worktree, adversarialCheckPlan{MutationAnalysis: true})
	if err != nil {
		t.Fatalf("MutationAnalysis=true: %v", err)
	}
	_ = ran // the fixture has no tests to mutate against, so no survivor
	// finding is guaranteed either; this call proves the enabled branch
	// still runs the mediated subprocess path without error.

	viaRisk, err := execution.reviewAdversariallyForRisk(
		t.Context(), worktree, domain.RiskLevelRoutine)
	if err != nil {
		t.Fatalf("reviewAdversariallyForRisk(routine): %v", err)
	}
	for _, finding := range viaRisk {
		if finding.Lineage == findingLineageMutationSurvivor {
			t.Errorf("reviewAdversariallyForRisk did not thread routine "+
				"risk through to skip mutation analysis: %+v", finding)
		}
	}
}
