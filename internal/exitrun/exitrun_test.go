package exitrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func cleanRoomFixture(t *testing.T) CleanRoom {
	t.Helper()
	root := t.TempDir()
	repository := filepath.Join(root, "reserveflow")
	if err := os.MkdirAll(filepath.Join(repository, ".git"), 0o755); err != nil {
		t.Fatalf("create repository fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repository, "main.go"),
		[]byte("package main\n"), 0o600); err != nil {
		t.Fatalf("write repository fixture: %v", err)
	}
	return CleanRoom{
		ProfileDirectory:    filepath.Join(root, "profile"),
		DatabaseSearchPaths: []string{filepath.Join(root, "profile", "codeflux.sqlite3")},
		RepositoryRoot:      repository,
		ExpectedRevision:    "abc1234567890abc1234567890abc1234567890a",
		HiddenTestPaths:     []string{"hidden/acceptance_test.go"},
		ArtifactVerified:    true,
		ConfiguredProviders: []string{"anthropic"},
		ObservationActive:   true,
		Environment:         map[string]string{"HOME": root, "PATH": "/usr/bin"},
	}
}

func fixedRevision(revision string) func(context.Context, string) (string, error) {
	return func(context.Context, string) (string, error) { return revision, nil }
}

// TestM24_001_009_CleanRoomPreconditionsAreCompleteAndChecked covers
// M24-001..009.
func TestM24_001_009_CleanRoomPreconditionsAreCompleteAndChecked(t *testing.T) {
	if err := ValidatePreconditions(); err != nil {
		t.Fatalf("preconditions are invalid: %v", err)
	}

	room := cleanRoomFixture(t)
	report, err := Verify(t.Context(), room, fixedRevision(room.ExpectedRevision))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !report.Ready() {
		t.Fatalf("a clean room was not ready: %+v", report.Unsatisfied())
	}
	if len(report.Results) != len(AllPreconditions()) {
		t.Fatalf("checked %d of %d preconditions", len(report.Results), len(AllPreconditions()))
	}
	if err := RequireReady(report); err != nil {
		t.Fatalf("a ready room was refused: %v", err)
	}

	// Every precondition must explain what a run would wrongly conclude
	// without it. One that changes no conclusion is a preference, not a
	// precondition.
	for _, precondition := range Preconditions() {
		if len(strings.Fields(precondition.WhyItMatters)) < 8 {
			t.Fatalf("precondition %q does not really say why it matters: %q",
				precondition.ID, precondition.WhyItMatters)
		}
	}
}

// TestM24_003_004_LeakedConfigurationAndPriorStateBlockTheRun is the property
// that makes a clean room clean.
func TestM24_003_004_LeakedConfigurationAndPriorStateBlockTheRun(t *testing.T) {
	// M24-003: any development variable is a leak.
	for _, name := range []string{
		"CODEFLUX_DATABASE", "codeflux_provider", "GOFLAGS",
		"PLAYWRIGHT_DRIVER_PATH", "GOCACHE",
	} {
		room := cleanRoomFixture(t)
		room.Environment[name] = "leaked"
		report, err := Verify(t.Context(), room, fixedRevision(room.ExpectedRevision))
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if report.Ready() {
			t.Fatalf("a run with %s visible was allowed", name)
		}
		if err := RequireReady(report); !errors.Is(err, ErrCleanRoomNotReady) {
			t.Fatalf("RequireReady returned %v", err)
		}
	}

	// M24-004: an existing database anywhere the run could find one blocks it.
	room := cleanRoomFixture(t)
	existing := filepath.Join(t.TempDir(), "codeflux.sqlite3")
	if err := os.WriteFile(existing, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatalf("write database fixture: %v", err)
	}
	room.DatabaseSearchPaths = append(room.DatabaseSearchPaths, existing)
	report, err := Verify(t.Context(), room, fixedRevision(room.ExpectedRevision))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if report.Ready() {
		t.Fatal("a run with an existing database was allowed")
	}

	// Searching nowhere is not evidence of absence.
	empty := cleanRoomFixture(t)
	empty.DatabaseSearchPaths = nil
	report, err = Verify(t.Context(), empty, fixedRevision(empty.ExpectedRevision))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if report.Ready() {
		t.Fatal("searching no locations was treated as finding no database")
	}
}

// TestM24_008_HiddenTestsMustBeSealedByNameNotOnlyByPath is the check that
// stops the exit run being circular.
func TestM24_008_HiddenTestsMustBeSealedByNameNotOnlyByPath(t *testing.T) {
	room := cleanRoomFixture(t)

	// At the declared path.
	atPath := filepath.Join(room.RepositoryRoot, "hidden")
	if err := os.MkdirAll(atPath, 0o755); err != nil {
		t.Fatalf("create hidden directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(atPath, "acceptance_test.go"),
		[]byte("package hidden\n"), 0o600); err != nil {
		t.Fatalf("write hidden test: %v", err)
	}
	report, err := Verify(t.Context(), room, fixedRevision(room.ExpectedRevision))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if report.Ready() {
		t.Fatal("a repository containing a hidden acceptance test was allowed")
	}

	// Moved somewhere else. A path-only check would pass here while the file is
	// just as readable to the agent.
	moved := cleanRoomFixture(t)
	elsewhere := filepath.Join(moved.RepositoryRoot, "internal", "sneaky")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, "acceptance_test.go"),
		[]byte("package sneaky\n"), 0o600); err != nil {
		t.Fatalf("write moved test: %v", err)
	}
	report, err = Verify(t.Context(), moved, fixedRevision(moved.ExpectedRevision))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if report.Ready() {
		t.Fatal("a hidden test moved to another path was not detected")
	}

	// Declaring no hidden tests cannot count as sealing them.
	undeclared := cleanRoomFixture(t)
	undeclared.HiddenTestPaths = nil
	report, err = Verify(t.Context(), undeclared, fixedRevision(undeclared.ExpectedRevision))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if report.Ready() {
		t.Fatal("declaring no hidden tests was treated as having sealed them")
	}
}

// TestM24_007_TheFrozenRevisionIsVerifiedNotAssumed covers M24-007.
func TestM24_007_TheFrozenRevisionIsVerifiedNotAssumed(t *testing.T) {
	room := cleanRoomFixture(t)

	report, err := Verify(t.Context(), room, fixedRevision("0000000000000000000000000000000000000000"))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if report.Ready() {
		t.Fatal("a repository at the wrong revision was allowed")
	}

	// No way to read the revision is a failure, not a pass.
	report, err = Verify(t.Context(), room, nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if report.Ready() {
		t.Fatal("an unreadable revision was treated as correct")
	}

	// So is declaring no expected revision.
	undeclared := cleanRoomFixture(t)
	undeclared.ExpectedRevision = ""
	report, err = Verify(t.Context(), undeclared, fixedRevision("anything"))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if report.Ready() {
		t.Fatal("a run with no declared frozen revision was allowed")
	}
}

// TestM24_009_ObservationIsRecommendedNotRequired proves the one non-blocking
// precondition behaves that way.
func TestM24_009_ObservationIsRecommendedNotRequired(t *testing.T) {
	room := cleanRoomFixture(t)
	room.ObservationActive = false
	report, err := Verify(t.Context(), room, fixedRevision(room.ExpectedRevision))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	// The run may proceed, but the gap must still be visible.
	if !report.Ready() {
		t.Fatal("a missing recording blocked the run")
	}
	found := false
	for _, result := range report.Unsatisfied() {
		if result.ID == PreObservationStarted {
			found = true
		}
	}
	if !found {
		t.Fatal("a missing recording was not reported at all")
	}
}

// TestM24_010_051_TheJourneyCoversEveryStepInOrder covers M24-010..051.
func TestM24_010_051_TheJourneyCoversEveryStepInOrder(t *testing.T) {
	if err := ValidateJourney(); err != nil {
		t.Fatalf("the journey is invalid: %v", err)
	}

	steps := Steps()
	if len(steps) != 42 {
		t.Fatalf("the journey has %d steps, want 42", len(steps))
	}
	for _, phase := range AllPhases() {
		if len(StepsFor(phase)) == 0 {
			t.Fatalf("phase %q has no steps", phase)
		}
	}

	// Every step must say what its failure would mean. A step whose failure
	// means nothing is not worth an evaluator's time.
	for _, step := range steps {
		if len(strings.Fields(step.FailureMeaning)) < 6 {
			t.Fatalf("%s does not really say what a failure would mean: %q",
				step.Todo, step.FailureMeaning)
		}
	}

	// The measurements the scorecard depends on must exist.
	measurements := Measurements()
	for _, required := range []string{
		"install-to-first-screen", "time-to-first-forecast", "time-to-first-plan",
		"time-to-first-diff", "forecast-error", "unexpected-events",
	} {
		found := false
		for _, name := range measurements {
			if name == required {
				found = true
			}
		}
		if !found {
			t.Fatalf("the journey never measures %q", required)
		}
	}
}

// TestM24_010_051_JourneyStepValidationIsLoadBearing proves an unusable step
// cannot enter the protocol.
func TestM24_010_051_JourneyStepValidationIsLoadBearing(t *testing.T) {
	valid := Steps()[0]
	corruptions := map[string]func(Step) Step{
		"foreign todo":    func(step Step) Step { step.Todo = "M23-001"; return step },
		"unknown phase":   func(step Step) Step { step.Phase = Phase("invented"); return step },
		"unknown kind":    func(step Step) Step { step.Kind = StepKind("invented"); return step },
		"no instruction":  func(step Step) Step { step.Instruction = ""; return step },
		"no expectation":  func(step Step) Step { step.Expected = ""; return step },
		"no failure note": func(step Step) Step { step.FailureMeaning = ""; return step },
		"unnamed measurement": func(step Step) Step {
			step.Kind = KindMeasure
			step.Measurement = ""
			return step
		},
		"measurement on a non-measure step": func(step Step) Step {
			step.Kind = KindAction
			step.Measurement = "something"
			return step
		},
	}
	for name, corrupt := range corruptions {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(valid).Validate(); err == nil {
				t.Fatalf("an unusable step validated: %s", name)
			}
		})
	}
}

// TestM24_010_051_ARunMustRecordEveryStepAndExplainEveryFailure covers the
// recording contract.
func TestM24_010_051_ARunMustRecordEveryStepAndExplainEveryFailure(t *testing.T) {
	complete := Run{}
	for _, step := range Steps() {
		outcome := Outcome{Todo: step.Todo, Passed: true}
		if step.Kind == KindMeasure {
			outcome.Value = 30 * time.Second
			outcome.Count = 1
		}
		complete.Outcomes = append(complete.Outcomes, outcome)
	}
	if err := complete.Validate(); err != nil {
		t.Fatalf("a complete run was rejected: %v", err)
	}
	measurement, err := complete.Measurement("time-to-first-diff")
	if err != nil {
		t.Fatalf("look up a measurement: %v", err)
	}
	if measurement.Value != 30*time.Second {
		t.Fatalf("measurement value = %v", measurement.Value)
	}
	if _, err := complete.Measurement("not-measured"); err == nil {
		t.Fatal("an unmeasured name resolved")
	}

	// A skipped step is refused: a partial walk cannot support a conclusion.
	partial := Run{Outcomes: complete.Outcomes[1:]}
	if err := partial.Validate(); err == nil {
		t.Fatal("a run that skipped a step validated")
	}

	// A failure with no note is refused: nobody can act on it afterwards.
	unexplained := Run{Outcomes: append([]Outcome(nil), complete.Outcomes...)}
	unexplained.Outcomes[0].Passed = false
	unexplained.Outcomes[0].Note = ""
	if err := unexplained.Validate(); err == nil {
		t.Fatal("an unexplained failure validated")
	}
	unexplained.Outcomes[0].Note = "the interface never appeared"
	if err := unexplained.Validate(); err != nil {
		t.Fatalf("an explained failure was rejected: %v", err)
	}
	if len(unexplained.Failures()) != 1 {
		t.Fatalf("failures = %+v", unexplained.Failures())
	}

	// A measurement that recorded nothing is refused.
	empty := Run{Outcomes: append([]Outcome(nil), complete.Outcomes...)}
	for index, outcome := range empty.Outcomes {
		if outcome.Todo == "M24-010" {
			empty.Outcomes[index].Value = 0
			empty.Outcomes[index].Count = 0
		}
	}
	if err := empty.Validate(); err == nil {
		t.Fatal("a measurement with no value validated")
	}

	// A duplicated step is refused.
	duplicated := Run{Outcomes: append(append([]Outcome(nil), complete.Outcomes...),
		complete.Outcomes[0])}
	if err := duplicated.Validate(); err == nil {
		t.Fatal("a run recording a step twice validated")
	}
}

// TestM24_052_061_IndependentEvaluationsMustBeIndependent covers M24-052..061.
func TestM24_052_061_IndependentEvaluationsMustBeIndependent(t *testing.T) {
	if err := ValidateEvaluations(); err != nil {
		t.Fatalf("evaluations are invalid: %v", err)
	}

	report := EvaluationReport{}
	for _, evaluation := range Evaluations() {
		report.Outcomes = append(report.Outcomes, EvaluationOutcome{
			ID: evaluation.ID, Passed: true,
			Evidence: "the hidden suite output and the diff",
			At:       time.Unix(1, 0).UTC(),
		})
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("a complete report was rejected: %v", err)
	}
	if len(report.BlockingFailures()) != 0 {
		t.Fatalf("a passing report reported failures: %v", report.BlockingFailures())
	}

	// The independence requirement is ENFORCED. A check that read the report it
	// was checking establishes only that the report agrees with itself.
	for _, evaluation := range Evaluations() {
		if !evaluation.IndependentOfReport {
			continue
		}
		tainted := EvaluationReport{Outcomes: append([]EvaluationOutcome(nil), report.Outcomes...)}
		for index, outcome := range tainted.Outcomes {
			if outcome.ID == evaluation.ID {
				tainted.Outcomes[index].ConsultedReport = true
			}
		}
		if err := tainted.Validate(); err == nil {
			t.Fatalf("evaluation %q consulted the report and still validated", evaluation.ID)
		}
	}

	// Only the two checks ABOUT the report may read it.
	for _, evaluation := range Evaluations() {
		if evaluation.IndependentOfReport {
			continue
		}
		if evaluation.ID != EvalClaimsBacked && evaluation.ID != EvalNoOverstatement {
			t.Fatalf("evaluation %q is allowed to read the report but is not about it",
				evaluation.ID)
		}
	}

	// A verdict with no evidence is an opinion.
	unevidenced := EvaluationReport{Outcomes: append([]EvaluationOutcome(nil), report.Outcomes...)}
	unevidenced.Outcomes[0].Evidence = ""
	if err := unevidenced.Validate(); err == nil {
		t.Fatal("an evaluation with no evidence validated")
	}

	// A skipped evaluation is refused.
	partial := EvaluationReport{Outcomes: report.Outcomes[1:]}
	if err := partial.Validate(); err == nil {
		t.Fatal("a report that skipped an evaluation validated")
	}

	// Blocking failures are surfaced.
	failed := EvaluationReport{Outcomes: append([]EvaluationOutcome(nil), report.Outcomes...)}
	failed.Outcomes[0].Passed = false
	if len(failed.BlockingFailures()) != 1 {
		t.Fatalf("blocking failures = %v", failed.BlockingFailures())
	}
}

// TestM24_062_071_RecoveryScenariosCoverTheRealFaults covers M24-062..071.
func TestM24_062_071_RecoveryScenariosCoverTheRealFaults(t *testing.T) {
	if err := ValidateRecoveryScenarios(); err != nil {
		t.Fatalf("recovery scenarios are invalid: %v", err)
	}
	scenarios := RecoveryScenarios()
	if len(scenarios) != 5 {
		t.Fatalf("%d recovery scenarios are declared, want 5", len(scenarios))
	}

	// Each must state a property AND the outcome that would fail the run.
	// Without the second, a scenario passes whenever nothing crashed.
	for _, scenario := range scenarios {
		if len(strings.Fields(scenario.WouldBeUnacceptable)) < 5 {
			t.Fatalf("scenario %q does not say what would fail the run: %q",
				scenario.ID, scenario.WouldBeUnacceptable)
		}
		if scenario.InjectTodo >= scenario.VerifyTodo {
			t.Fatalf("scenario %q verifies before it injects", scenario.ID)
		}
	}

	// The set must cover the four fault sources that matter, not four
	// variations of one.
	covered := map[ScenarioID]bool{}
	for _, scenario := range scenarios {
		covered[scenario.ID] = true
	}
	for _, required := range []ScenarioID{
		ScenarioBrowserDisconnect, ScenarioWorkerTermination,
		ScenarioCoordinatorKill, ScenarioBudgetExhaustion, ScenarioConcurrentUserEdit,
	} {
		if !covered[required] {
			t.Fatalf("no scenario covers %q", required)
		}
	}

	corrupt := scenarios[0]
	corrupt.VerifyTodo = corrupt.InjectTodo
	if err := corrupt.Validate(); err == nil {
		t.Fatal("a scenario injecting and verifying under one TODO validated")
	}
}

// TestM24_072_079_MemoryChecksCoverEligibilityAndLineage covers M24-072..079.
func TestM24_072_079_MemoryChecksCoverEligibilityAndLineage(t *testing.T) {
	if err := ValidateMemoryChecks(); err != nil {
		t.Fatalf("memory checks are invalid: %v", err)
	}
	checks := MemoryChecks()
	if len(checks) != 8 {
		t.Fatalf("%d memory checks are declared, want 8", len(checks))
	}

	// The two properties the plan's prohibited dependencies turn on must both
	// be checked.
	byID := map[MemoryCheckID]MemoryCheck{}
	for _, check := range checks {
		byID[check.ID] = check
	}
	authority := byID[MemoryNoAuthority]
	if !strings.Contains(authority.AcceptableAnswer, "never confers eligibility") {
		t.Fatalf("the similarity check does not assert the prohibition: %q",
			authority.AcceptableAnswer)
	}
	invalidation := byID[MemoryInvalidation]
	for _, phrase := range []string{"quarantined automatically", "flagged for review"} {
		if !strings.Contains(invalidation.AcceptableAnswer, phrase) {
			t.Fatalf("the invalidation check does not distinguish lineage kinds: %q",
				invalidation.AcceptableAnswer)
		}
	}
	boundary := byID[MemoryProjectBound]
	if !strings.Contains(boundary.AcceptableAnswer, "no item from another project") {
		t.Fatalf("the boundary check is not absolute: %q", boundary.AcceptableAnswer)
	}
}

func completeScorecard() Scorecard {
	card := Scorecard{
		SourceRevision:  "abc1234567890abc1234567890abc1234567890a",
		MethodologyPath: "docs/benchmarks.md",
		At:              time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		Workarounds:     []string{"restarted the browser once after a stalled render"},
		Flaky:           []string{"the graph layout benchmark varied by 30% between runs"},
		MisleadingClaims: []string{
			"the phrase 'verified' on the review screen could be read as stronger than it is",
		},
		PlanUpdates: []string{
			"§31: recorded that influence lineage was observed working end to end",
		},
	}
	for _, group := range AllMetricGroups() {
		card.Sections = append(card.Sections, Section{
			Group: group, Populated: true,
			Findings: []string{"observed and recorded"},
		})
	}
	for _, criterion := range DeclaredKillCriteria() {
		criterion.Evidence = "checked against the run's recorded results"
		card.KillCriteria = append(card.KillCriteria, criterion)
	}
	for _, subsystem := range AllSubsystems() {
		card.Decisions = append(card.Decisions, Decision{
			Subsystem: subsystem, Verdict: VerdictContinue,
			Rationale:    "behaved as designed across the run",
			EvidenceRefs: []string{"correctness", "recovery"},
		})
	}
	card.GatedFeatures = DeclaredGatedFeatures()
	return card
}

// TestM24_080_100_TheScorecardCannotBeIncomplete covers M24-080..100.
func TestM24_080_100_TheScorecardCannotBeIncomplete(t *testing.T) {
	card := completeScorecard()
	if err := card.Validate(); err != nil {
		t.Fatalf("a complete scorecard was rejected: %v", err)
	}

	verdict, reason, err := card.Decide()
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if verdict != ExitPassed {
		t.Fatalf("a clean run decided %q: %s", verdict, reason)
	}

	// M24-080..088: every group must be populated.
	for _, group := range AllMetricGroups() {
		missing := completeScorecard()
		var kept []Section
		for _, section := range missing.Sections {
			if section.Group != group {
				kept = append(kept, section)
			}
		}
		missing.Sections = kept
		if err := missing.Validate(); err == nil {
			t.Fatalf("a scorecard missing group %q validated", group)
		}
	}

	// M24-093: every kill criterion must be compared against, with evidence.
	for _, criterion := range DeclaredKillCriteria() {
		missing := completeScorecard()
		var kept []KillCriterion
		for _, candidate := range missing.KillCriteria {
			if candidate.Name != criterion.Name {
				kept = append(kept, candidate)
			}
		}
		missing.KillCriteria = kept
		if err := missing.Validate(); err == nil {
			t.Fatalf("a scorecard skipping criterion %q validated", criterion.Name)
		}
	}
	unevidenced := completeScorecard()
	unevidenced.KillCriteria[0].Evidence = ""
	if err := unevidenced.Validate(); err == nil {
		t.Fatal("a kill criterion recorded without evidence validated")
	}

	// M24-094: every subsystem needs a decision, and every decision needs
	// evidence.
	for _, subsystem := range AllSubsystems() {
		missing := completeScorecard()
		var kept []Decision
		for _, decision := range missing.Decisions {
			if decision.Subsystem != subsystem {
				kept = append(kept, decision)
			}
		}
		missing.Decisions = kept
		if err := missing.Validate(); err == nil {
			t.Fatalf("a scorecard with no decision for %q validated", subsystem)
		}
	}
	preference := completeScorecard()
	preference.Decisions[0].EvidenceRefs = nil
	if err := preference.Validate(); err == nil {
		t.Fatal("a decision with no evidence validated")
	}

	// M24-098, M24-099, M24-100.
	for name, corrupt := range map[string]func(Scorecard) Scorecard{
		"no source revision": func(candidate Scorecard) Scorecard {
			candidate.SourceRevision = ""
			return candidate
		},
		"no archived methodology": func(candidate Scorecard) Scorecard {
			candidate.MethodologyPath = ""
			return candidate
		},
		"no plan updates": func(candidate Scorecard) Scorecard {
			candidate.PlanUpdates = nil
			return candidate
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(completeScorecard()).Validate(); err == nil {
				t.Fatalf("an incomplete scorecard validated: %s", name)
			}
		})
	}
}

// TestM24_092_FailuresAreClassifiedWithEvidence covers M24-092.
func TestM24_092_FailuresAreClassifiedWithEvidence(t *testing.T) {
	// Every class must imply a different next action, or the classification
	// does no work.
	remedies := map[string]bool{}
	for _, class := range AllFailureClasses() {
		remedy := class.Remedy()
		if strings.TrimSpace(remedy) == "" {
			t.Fatalf("class %q implies no remedy", class)
		}
		if remedies[remedy] {
			t.Fatalf("class %q shares a remedy with another class", class)
		}
		remedies[remedy] = true
	}
	if FailureClass("invented").Valid() {
		t.Fatal("an unknown failure class validated")
	}

	valid := Failure{
		Summary: "the plan omitted a required migration",
		Class:   ClassSpecification,
		Evidence: "the plan text lists no migration and the acceptance test requires the " +
			"new column",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a valid failure was rejected: %v", err)
	}
	for name, corrupt := range map[string]func(Failure) Failure{
		"no summary":  func(failure Failure) Failure { failure.Summary = ""; return failure },
		"no class":    func(failure Failure) Failure { failure.Class = ""; return failure },
		"no evidence": func(failure Failure) Failure { failure.Evidence = ""; return failure },
	} {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(valid).Validate(); err == nil {
				t.Fatalf("an unusable failure validated: %s", name)
			}
		})
	}
}

// TestM24_093_094_KillCriteriaAndStopDecisionsBlockTheExit covers M24-093 and
// M24-094.
func TestM24_093_094_KillCriteriaAndStopDecisionsBlockTheExit(t *testing.T) {
	triggered := completeScorecard()
	triggered.KillCriteria[0].Triggered = true
	verdict, reason, err := triggered.Decide()
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if verdict != ExitBlocked {
		t.Fatalf("a triggered kill criterion decided %q", verdict)
	}
	if !strings.Contains(reason, triggered.KillCriteria[0].Name) {
		t.Fatalf("the reason does not name the criterion: %q", reason)
	}

	stopped := completeScorecard()
	stopped.Decisions[0].Verdict = VerdictStop
	verdict, reason, err = stopped.Decide()
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if verdict != ExitBlocked {
		t.Fatalf("a stopped subsystem decided %q", verdict)
	}
	if !strings.Contains(reason, string(stopped.Decisions[0].Subsystem)) {
		t.Fatalf("the reason does not name the subsystem: %q", reason)
	}

	// A flawed experiment is checked FIRST: if the run was broken, neither a
	// pass nor a failure means anything.
	flawed := completeScorecard()
	flawed.KillCriteria[0].Triggered = true
	flawed.Failures = append(flawed.Failures, Failure{
		Summary:  "the frozen repository had local modifications",
		Class:    ClassExperiment,
		Evidence: "git status showed two modified files before the run began",
	})
	verdict, reason, err = flawed.Decide()
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if verdict != ExitInconclusive {
		t.Fatalf("a flawed experiment decided %q: %s", verdict, reason)
	}
	if !strings.Contains(reason, "repeat it") {
		t.Fatalf("the reason does not say to repeat the run: %q", reason)
	}
}

// TestM24_095_096_GatedFeaturesStayDisabledWithoutTheirGate covers M24-095 and
// M24-096.
func TestM24_095_096_GatedFeaturesStayDisabledWithoutTheirGate(t *testing.T) {
	features := DeclaredGatedFeatures()
	if len(features) != 2 {
		t.Fatalf("%d gated features are declared, want 2", len(features))
	}
	for _, feature := range features {
		if feature.Enabled {
			t.Fatalf("feature %q is enabled by default", feature.Name)
		}
		if err := feature.Validate(); err != nil {
			t.Fatalf("a declared feature is invalid: %v", err)
		}
		// Enabling without the gate must be refused outright.
		enabled := feature
		enabled.Enabled = true
		if err := enabled.Validate(); err == nil {
			t.Fatalf("feature %q was enabled without its gate", feature.Name)
		}
		// With the gate met it is allowed, or the gate is unreachable.
		enabled.GateMet = true
		if err := enabled.Validate(); err != nil {
			t.Fatalf("feature %q could not be enabled even with its gate: %v",
				feature.Name, err)
		}
	}

	// A scorecard that does not account for a gated feature is incomplete.
	unaccounted := completeScorecard()
	unaccounted.GatedFeatures = unaccounted.GatedFeatures[1:]
	if err := unaccounted.Validate(); err == nil {
		t.Fatal("a scorecard ignoring a gated feature validated")
	}
}

// TestM24_097_TheDefectListIsPrioritisedAndClassified covers M24-097.
func TestM24_097_TheDefectListIsPrioritisedAndClassified(t *testing.T) {
	valid := Defect{
		Summary:  "the doctor's provider check cannot distinguish a proxy failure",
		Class:    ClassImplementation,
		Priority: 2,
		Impact:   "a user behind a proxy is told to replace a credential that was correct",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a valid defect was rejected: %v", err)
	}
	for name, corrupt := range map[string]func(Defect) Defect{
		"no summary":  func(defect Defect) Defect { defect.Summary = ""; return defect },
		"no class":    func(defect Defect) Defect { defect.Class = ""; return defect },
		"no priority": func(defect Defect) Defect { defect.Priority = 0; return defect },
		"no impact":   func(defect Defect) Defect { defect.Impact = ""; return defect },
	} {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(valid).Validate(); err == nil {
				t.Fatalf("an unusable defect validated: %s", name)
			}
		})
	}

	card := completeScorecard()
	card.Defects = []Defect{valid, {Summary: "x", Class: ClassUX}}
	if err := card.Validate(); err == nil {
		t.Fatal("a scorecard with an unprioritised defect validated")
	}
}
