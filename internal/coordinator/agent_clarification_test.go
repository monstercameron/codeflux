package coordinator

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
)

// analysisOf reads a requirement the way the product does.
func analysisOf(t *testing.T, requirement string) storage.RequirementAnalysis {
	t.Helper()
	analysis, err := storage.AnalyzeTaskRequirement(requirement)
	if err != nil {
		t.Fatalf("the product could not read its own requirement: %v", err)
	}
	return analysis
}

// TestAnAmbiguousRequestIsPutToThePersonRatherThanGuessed covers the posture a
// supervised session wants.
func TestAnAmbiguousRequestIsPutToThePersonRatherThanGuessed(t *testing.T) {
	// Two readings that lead to different work. There is no defensible default
	// here: whichever the run picked, half the time it would build the wrong
	// thing and the person would find out at review.
	analysis := analysisOf(t,
		"Add caching to the report page, either in the handler or in the store.")
	if !analysis.RequiresClarification() {
		t.Fatal("the product no longer reads this request as ambiguous, so " +
			"this test is checking the wrong thing")
	}

	decision := resolveAmbiguity(analysis, domain.AmbiguityAsk)
	if !decision.Blocked {
		t.Fatal("an ambiguous request was allowed to proceed on a guess")
	}
	if strings.TrimSpace(decision.Question) == "" {
		t.Error("the run blocked without asking anything, which is the worst " +
			"of both: no work and no question")
	}
}

// TestAssumingStillRefusesToInventAnAnswerItCannotDefault is the limit of the
// unattended posture.
//
// Assume means "take the narrowest defensible reading", not "pick one". When
// the readings lead to different work there is nothing to narrow to, and a run
// that picked anyway would be guessing with the person's repository.
func TestAssumingStillRefusesToInventAnAnswerItCannotDefault(t *testing.T) {
	analysis := analysisOf(t,
		"Add caching to the report page, either in the handler or in the store.")

	decision := resolveAmbiguity(analysis, domain.AmbiguityAssume)
	if !decision.Blocked {
		t.Fatal("a question with no defensible default was answered by the " +
			"machine rather than the person")
	}
	if !strings.Contains(decision.Question, "different work") {
		t.Errorf("the run did not say why it could not default: %q",
			decision.Question)
	}
}

// TestABoundedReadingIsStatedAndTheWorkProceeds covers the common case.
//
// Most vagueness is not a fork in the road: "if needed" has an obvious
// narrowest reading, and stopping to ask about it would make the product
// exhausting to use. The reading is stated where the person will see it, and
// the work goes ahead.
func TestABoundedReadingIsStatedAndTheWorkProceeds(t *testing.T) {
	analysis := analysisOf(t,
		"Add a retry to the upload handler and adjust the timeout if needed.")
	if analysis.RequiresClarification() {
		t.Fatal("a bounded reading is being treated as a blocking question")
	}

	decision := resolveAmbiguity(analysis, domain.AmbiguityAsk)
	if decision.Blocked {
		t.Error("the run stopped to ask about something it could safely default")
	}
	if len(decision.Assumptions) == 0 {
		t.Error("the run proceeded on a reading it never stated, so the person " +
			"cannot tell what it decided on their behalf")
	}
}

// TestAPlainRequestIsNotInterrogated guards against the opposite failure.
//
// A product that asks a question about an unambiguous request is worse than
// one that never asks: it teaches people to dismiss the questions.
func TestAPlainRequestIsNotInterrogated(t *testing.T) {
	analysis := analysisOf(t,
		"Create cmd/greet/main.go so the program prints a greeting.")

	for _, policy := range []domain.AmbiguityPolicy{
		domain.AmbiguityAsk, domain.AmbiguityAssume,
	} {
		decision := resolveAmbiguity(analysis, policy)
		if decision.Blocked {
			t.Errorf("%s stopped to ask about a request that reads one way: %q",
				policy, decision.Question)
		}
		if len(decision.Assumptions) != 0 {
			t.Errorf("%s invented assumptions for a plain request: %v",
				policy, decision.Assumptions)
		}
	}
}

// TestTheDefaultPostureIsToAsk records the choice itself.
//
// A coordinator that proceeded on guesses unless told otherwise would make
// every unattended run a silent risk, so the default is pinned here rather
// than left to whoever next edits the settings.
func TestTheDefaultPostureIsToAsk(t *testing.T) {
	execution := &AgentExecution{settings: pipeline.DefaultSettings()}
	if execution.ambiguityPolicy() != domain.AmbiguityAsk {
		t.Errorf("the default posture is %q", execution.ambiguityPolicy())
	}
	if _, err := execution.WithSettings(pipeline.Settings{}); err == nil {
		t.Error("an unconfigured settings value was accepted")
	}
	assuming := pipeline.DefaultSettings()
	assuming.Ambiguity = pipeline.AmbiguityAssume
	if _, err := execution.WithSettings(assuming); err != nil {
		t.Fatalf("a valid posture was refused: %v", err)
	}
	if execution.ambiguityPolicy() != domain.AmbiguityAssume {
		t.Error("a declared posture was ignored")
	}
}
