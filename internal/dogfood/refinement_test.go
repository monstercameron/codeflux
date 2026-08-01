package dogfood

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestM24_161_ObservationSetCoversEveryRequiredQuestion(t *testing.T) {
	if err := ValidateObservations(); err != nil {
		t.Fatalf("declared observations are not valid: %v", err)
	}
	if got, want := len(Observations()), len(AllObservations()); got != want {
		t.Fatalf("declared %d observations for %d ids", got, want)
	}
}

func TestM24_162_ObservationSetRecordsWhatDidNotWork(t *testing.T) {
	// The two observations whose value is a negative finding are the graph-mode
	// audit and the opacity inventory. If either stopped being able to report a
	// failure, the set could only ever confirm the design.
	negatives := map[ObservationID]bool{}
	for _, observation := range Observations() {
		if observation.RecordsNegative {
			negatives[observation.ID] = true
		}
	}
	for _, id := range []ObservationID{ObserveGraphModes, ObserveOpacity} {
		if !negatives[id] {
			t.Errorf("observation %q no longer records a negative finding", id)
		}
	}
}

func TestM24_170_OpacityObservationNamesTheThingsAnOperatorMustSee(t *testing.T) {
	var opacity Observation
	for _, observation := range Observations() {
		if observation.ID == ObserveOpacity {
			opacity = observation
		}
	}
	for _, subject := range []string{
		"state", "authority", "cost", "next action", "failure", "recovery",
	} {
		if !strings.Contains(opacity.AcceptableAnswer, subject) {
			t.Errorf("the opacity observation does not cover %q", subject)
		}
	}
}

func TestM24_174_OnlyCodefluxOwnedFailuresAreRepairableHere(t *testing.T) {
	for _, ownership := range AllOwnerships() {
		if !ownership.Valid() {
			t.Fatalf("declared ownership %q reports itself invalid", ownership)
		}
		repairable := ownership.RepairableHere()
		if ownership == OwnerCodeflux && !repairable {
			t.Error("a CodeFlux-owned failure must be repairable in CodeFlux")
		}
		if ownership != OwnerCodeflux && repairable {
			t.Errorf(
				"ownership %q reports itself repairable in CodeFlux; repairing it here "+
					"produces a change that helps nothing", ownership)
		}
	}
	if Ownership("unspecified").Valid() {
		t.Error("an undeclared ownership was accepted")
	}
}

func TestM24_174_ProtocolAndHiddenTestFailuresInvalidateTheRun(t *testing.T) {
	for _, ownership := range AllOwnerships() {
		want := ownership == OwnerProtocol || ownership == OwnerHiddenTest
		if got := ownership.InvalidatesRun(); got != want {
			t.Errorf("ownership %q reports InvalidatesRun=%v, want %v", ownership, got, want)
		}
	}
}

func completeFrozenEvidence() FrozenEvidence {
	return FrozenEvidence{
		EventSequence: "events 1-482 for episode 3",
		WorktreeDiff:  "diff of 4 files against the accepted base",
		ProviderModel: "recorded-provider/model-a",
		Policy:        "policy snapshot at task start",
		Budget:        "budget ledger at failure",
		Environment:   "windows/arm64, go1.26.3",
		ToolVersions:  "git 2.47, sqlite 3.46",
		Diagnostics:   "bundle 2026-08-01T00:00Z",
		FrozenAt:      time.Unix(1_780_000_000, 0).UTC(),
	}
}

func completeFailureReport() FailureReport {
	return FailureReport{
		ID:               "F-001",
		Task:             3,
		AcceptedBase:     "accepted-base-3",
		CodefluxVersion:  "0.1.0+dogfood.7",
		Run:              "run-2",
		Episode:          "episode-3",
		EvaluatorResult:  "hidden assertion 4 failed",
		Frozen:           completeFrozenEvidence(),
		Category:         "authority",
		Severity:         "blocking",
		Frequency:        "1 of 3 repetitions",
		Reproducibility:  "reproducible outside the run",
		Symptom:          "an expired approval was still honoured on replay",
		ResponsibleLayer: 9,
		Ownership:        OwnerCodeflux,
	}
}

func TestM24_171_AFailureReportIdentifiesExactlyWhatWasRunning(t *testing.T) {
	if err := completeFailureReport().Validate(); err != nil {
		t.Fatalf("a complete report was rejected: %v", err)
	}

	for name, damage := range map[string]func(*FailureReport){
		"no identity":   func(report *FailureReport) { report.ID = "" },
		"no base":       func(report *FailureReport) { report.AcceptedBase = "" },
		"no version":    func(report *FailureReport) { report.CodefluxVersion = "" },
		"no run":        func(report *FailureReport) { report.Run = "" },
		"no episode":    func(report *FailureReport) { report.Episode = "" },
		"no verdict":    func(report *FailureReport) { report.EvaluatorResult = "" },
		"task zero":     func(report *FailureReport) { report.Task = 0 },
		"task past end": func(report *FailureReport) { report.Task = PacketCount + 1 },
		"unknown owner": func(report *FailureReport) { report.Ownership = "somebody" },
		"layer past 20": func(report *FailureReport) { report.ResponsibleLayer = 21 },
	} {
		t.Run(name, func(t *testing.T) {
			report := completeFailureReport()
			damage(&report)
			if err := report.Validate(); err == nil {
				t.Fatalf("a report with %s was accepted", name)
			}
		})
	}
}

func TestM24_172_RepairCannotBeginBeforeEvidenceIsFrozen(t *testing.T) {
	// Each field is dropped individually because the error must name the gap:
	// "evidence incomplete" tells nobody which state was already destroyed.
	for name, damage := range map[string]func(*FrozenEvidence){
		"event-sequence":     func(frozen *FrozenEvidence) { frozen.EventSequence = "" },
		"worktree-diff":      func(frozen *FrozenEvidence) { frozen.WorktreeDiff = "" },
		"provider-and-model": func(frozen *FrozenEvidence) { frozen.ProviderModel = "" },
		"policy":             func(frozen *FrozenEvidence) { frozen.Policy = "" },
		"budget":             func(frozen *FrozenEvidence) { frozen.Budget = "" },
		"environment":        func(frozen *FrozenEvidence) { frozen.Environment = "" },
		"tool-versions":      func(frozen *FrozenEvidence) { frozen.ToolVersions = "" },
		"diagnostics":        func(frozen *FrozenEvidence) { frozen.Diagnostics = "" },
	} {
		t.Run(name, func(t *testing.T) {
			frozen := completeFrozenEvidence()
			damage(&frozen)
			err := frozen.Validate()
			if err == nil {
				t.Fatalf("evidence missing %s was accepted as frozen", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the error does not name the missing field %s: %v", name, err)
			}
		})
	}

	frozen := completeFrozenEvidence()
	frozen.FrozenAt = time.Time{}
	if err := frozen.Validate(); err == nil {
		t.Error("evidence with no freeze time was accepted")
	}
}

func TestM24_175_WorkaroundsThatChangeTheQuestionContaminateTheRun(t *testing.T) {
	clean := completeFailureReport()
	clean.Workarounds = []Workaround{{Description: "retried the same command once"}}
	if err := clean.Validate(); err != nil {
		t.Fatalf("a report with a clean workaround was rejected: %v", err)
	}
	if clean.Contaminating() {
		t.Error("an ordinary retry was reported as contaminating")
	}

	for name, workaround := range map[string]Workaround{
		"manual intervention": {Description: "edited the worktree by hand", Contaminated: true},
		"moved goalposts": {
			Description: "relaxed the acceptance threshold", ChangedAcceptance: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			report := completeFailureReport()
			report.Workarounds = []Workaround{workaround}
			if !report.Contaminating() {
				t.Fatalf("%s was not reported as contaminating", name)
			}
		})
	}

	undescribed := completeFailureReport()
	undescribed.Workarounds = []Workaround{{Contaminated: true}}
	if err := undescribed.Validate(); err == nil {
		t.Error("a workaround with no description was accepted")
	}
}

func TestM24_176_RepairStagesMapOntoTheirTodosInOrder(t *testing.T) {
	protocol := RepairProtocol()
	if got, want := len(protocol), 14; got != want {
		t.Fatalf("the protocol has %d stages, M24-176..190 declares %d", got, want)
	}
	seen := map[RepairStage]bool{}
	for index, stage := range protocol {
		if seen[stage] {
			t.Fatalf("stage %q appears twice", stage)
		}
		seen[stage] = true
		if got, want := StageTodo(stage), fmt.Sprintf("M24-%d", 176+index); got != want {
			t.Errorf("stage %q maps to %s, want %s", stage, got, want)
		}
	}
	if StageTodo("invented-stage") != "" {
		t.Error("an undeclared stage was given a TODO")
	}
}

func completeRepair() Repair {
	tradeoffs := map[string]string{}
	for _, dimension := range TradeoffDimensions() {
		tradeoffs[dimension] = "recorded for " + dimension
	}
	return Repair{
		FailureID:    "F-001",
		Completed:    RepairProtocol(),
		FailureClass: "authority decisions replayed without re-evaluating expiry",
		Invariant:    "an approval's validity is decided at use, never at record time",
		Tradeoffs:    tradeoffs,
		Closure:      ClosureFixed,
	}
}

func TestM24_179_ARepairMayNotBuyAPassByWeakeningAPolicy(t *testing.T) {
	if err := completeRepair().Validate(); err != nil {
		t.Fatalf("a complete repair was rejected: %v", err)
	}

	for name, weaken := range map[string]func(*Repair){
		"validation":       func(repair *Repair) { repair.WeakenedValidation = true },
		"permission":       func(repair *Repair) { repair.WeakenedPermission = true },
		"evidence":         func(repair *Repair) { repair.WeakenedEvidence = true },
		"budget":           func(repair *Repair) { repair.WeakenedBudget = true },
		"project-boundary": func(repair *Repair) { repair.WeakenedProjectBoundary = true },
		"recovery":         func(repair *Repair) { repair.WeakenedRecovery = true },
	} {
		t.Run(name, func(t *testing.T) {
			repair := completeRepair()
			weaken(&repair)
			err := repair.Validate()
			if err == nil {
				t.Fatalf("a repair that weakened %s policy was accepted", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the error does not name the weakened policy: %v", err)
			}
		})
	}
}

func TestM24_188_TheThreeShortcutsAreRefused(t *testing.T) {
	for name, shortcut := range map[string]func(*Repair){
		"passes only the hidden case": func(repair *Repair) { repair.PassesOnlyHiddenCase = true },
		"uses future knowledge":       func(repair *Repair) { repair.UsesFutureKnowledge = true },
		"adds task-specific prompt":   func(repair *Repair) { repair.AddsTaskSpecificPrompt = true },
	} {
		t.Run(name, func(t *testing.T) {
			repair := completeRepair()
			shortcut(&repair)
			if err := repair.Validate(); err == nil {
				t.Fatalf("a repair that %s was accepted", name)
			}
		})
	}
}

func TestM24_178_ARepairWithoutAClassIsAPatch(t *testing.T) {
	for name, damage := range map[string]func(*Repair){
		"no failure class": func(repair *Repair) { repair.FailureClass = "" },
		"no invariant":     func(repair *Repair) { repair.Invariant = "" },
		"no failure id":    func(repair *Repair) { repair.FailureID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			repair := completeRepair()
			damage(&repair)
			if err := repair.Validate(); err == nil {
				t.Fatalf("a repair with %s was accepted", name)
			}
		})
	}
}

func TestM24_189_TradeoffsAreRecordedBeforeClosing(t *testing.T) {
	for _, dimension := range TradeoffDimensions() {
		t.Run(dimension, func(t *testing.T) {
			repair := completeRepair()
			delete(repair.Tradeoffs, dimension)
			err := repair.Validate()
			if err == nil {
				t.Fatalf("a repair closed without recording %s", dimension)
			}
			if !strings.Contains(err.Error(), dimension) {
				t.Errorf("the error does not name the unrecorded dimension: %v", err)
			}
		})
	}
}

func TestM24_190_EveryDefectClosesInADeclaredWay(t *testing.T) {
	for _, closure := range AllClosures() {
		if !closure.Valid() {
			t.Fatalf("declared closure %q reports itself invalid", closure)
		}
		repair := completeRepair()
		repair.Closure = closure
		if err := repair.Validate(); err != nil {
			t.Errorf("closure %q was rejected: %v", closure, err)
		}
	}
	// There is deliberately no way to close a defect by dropping it.
	for _, absent := range []Closure{"", "silently-discarded", "forgotten"} {
		repair := completeRepair()
		repair.Closure = absent
		if err := repair.Validate(); err == nil {
			t.Errorf("a defect closed as %q was accepted", absent)
		}
	}
}

func TestM24_180_TheRepairProtocolMustBeFollowedInOrder(t *testing.T) {
	t.Run("skipped stage", func(t *testing.T) {
		for index := range RepairProtocol() {
			repair := completeRepair()
			stages := RepairProtocol()
			skipped := stages[index]
			repair.Completed = append(append([]RepairStage{}, stages[:index]...),
				stages[index+1:]...)
			err := repair.Validate()
			if err == nil {
				t.Fatalf("a repair that skipped %q was accepted", skipped)
			}
			if !strings.Contains(err.Error(), string(skipped)) {
				t.Errorf("the error does not name the skipped stage %q: %v", skipped, err)
			}
		}
	})

	t.Run("implemented before reproducing", func(t *testing.T) {
		repair := completeRepair()
		reordered := append([]RepairStage{}, RepairProtocol()...)
		// Move the implementation ahead of the reproduction: the classic way to
		// end up with a repair nobody can show works.
		reordered[0], reordered[3] = reordered[3], reordered[0]
		repair.Completed = reordered
		if err := repair.Validate(); err == nil {
			t.Fatal("a repair implemented before reproducing was accepted")
		}
	})

	t.Run("duplicate stage", func(t *testing.T) {
		repair := completeRepair()
		repair.Completed = append(repair.Completed, StageClose)
		if err := repair.Validate(); err == nil {
			t.Fatal("a repair that closed twice was accepted")
		}
	})

	t.Run("unknown stage", func(t *testing.T) {
		repair := completeRepair()
		repair.Completed = append(repair.Completed, "shipped-it")
		if err := repair.Validate(); err == nil {
			t.Fatal("a repair with an undeclared stage was accepted")
		}
	})
}

func frozenReviewer() ReviewerConfig {
	return ReviewerConfig{
		Prompt:          "review the diff against the stated requirement only",
		Model:           "recorded-provider/model-r",
		InputAllowlist:  []string{"worktree-diff", "requirement-text", "validation-output"},
		OutputSchema:    "findings[]{severity,location,claim}",
		ExecutionTiming: "after the run's acceptance verdict is recorded",
		BudgetMinor:     500,
		CostAccounting:  "charged to the evaluation budget, not the task budget",
		FrozenAt:        time.Unix(1_780_000_000, 0).UTC(),
	}
}

func TestM24_216_TheReviewerIsFrozenAndCannotAct(t *testing.T) {
	if err := frozenReviewer().Validate(); err != nil {
		t.Fatalf("a frozen evaluation-only reviewer was rejected: %v", err)
	}

	for name, damage := range map[string]func(*ReviewerConfig){
		"no prompt":       func(config *ReviewerConfig) { config.Prompt = "" },
		"no model":        func(config *ReviewerConfig) { config.Model = "" },
		"no allowlist":    func(config *ReviewerConfig) { config.InputAllowlist = nil },
		"no schema":       func(config *ReviewerConfig) { config.OutputSchema = "" },
		"no timing":       func(config *ReviewerConfig) { config.ExecutionTiming = "" },
		"no budget":       func(config *ReviewerConfig) { config.BudgetMinor = 0 },
		"no accounting":   func(config *ReviewerConfig) { config.CostAccounting = "" },
		"no freeze time":  func(config *ReviewerConfig) { config.FrozenAt = time.Time{} },
		"can edit":        func(config *ReviewerConfig) { config.CanEdit = true },
		"can approve":     func(config *ReviewerConfig) { config.CanApprove = true },
		"negative budget": func(config *ReviewerConfig) { config.BudgetMinor = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			config := frozenReviewer()
			damage(&config)
			if err := config.Validate(); err == nil {
				t.Fatalf("a reviewer with %s was accepted", name)
			}
		})
	}
}

func TestM24_217_TheReviewerCannotReachTheAnswers(t *testing.T) {
	for _, reachable := range []string{
		"evaluator-verdicts", "hidden-assertions", "answer-key",
		"future-packets", "packet-8", "EVALUATOR-LOG",
	} {
		t.Run(reachable, func(t *testing.T) {
			config := frozenReviewer()
			config.InputAllowlist = append(config.InputAllowlist, reachable)
			if err := config.Validate(); err == nil {
				t.Fatalf("a reviewer allowed to read %q was accepted", reachable)
			}
		})
	}
}

func completePreregistration() Preregistration {
	return Preregistration{
		CandidateVersion:            "prompt-v4",
		Diff:                        "state the invariant before proposing an edit",
		TuningCohort:                "tasks 1-6",
		HeldOutCohort:               "tasks 7-15",
		PrimaryEndpoint:             "independent acceptance rate",
		MinimumEffect:               "at least 10 points on the held-out cohort",
		Repetitions:                 3,
		Analysis:                    "per-task paired comparison, no post-hoc stratification",
		StopRule:                    "stop after 3 repetitions regardless of direction",
		MultipleComparisonTreatment: "one primary endpoint; all others reported as exploratory",
		ExecutionEnvelope:           "same provider, model, policy, and budget as the baseline",
	}
}

func TestM24_219_APreregistrationMustConstrainEveryChoosableField(t *testing.T) {
	if err := completePreregistration().Validate(); err != nil {
		t.Fatalf("a complete preregistration was rejected: %v", err)
	}

	for name, damage := range map[string]func(*Preregistration){
		"candidate-version":   func(reg *Preregistration) { reg.CandidateVersion = "" },
		"diff":                func(reg *Preregistration) { reg.Diff = "" },
		"tuning-cohort":       func(reg *Preregistration) { reg.TuningCohort = "" },
		"held-out-cohort":     func(reg *Preregistration) { reg.HeldOutCohort = "" },
		"primary-endpoint":    func(reg *Preregistration) { reg.PrimaryEndpoint = "" },
		"minimum-effect":      func(reg *Preregistration) { reg.MinimumEffect = "" },
		"analysis":            func(reg *Preregistration) { reg.Analysis = "" },
		"stop-rule":           func(reg *Preregistration) { reg.StopRule = "" },
		"multiple-comparison": func(reg *Preregistration) { reg.MultipleComparisonTreatment = "" },
		"execution-envelope":  func(reg *Preregistration) { reg.ExecutionEnvelope = "" },
	} {
		t.Run(name, func(t *testing.T) {
			registration := completePreregistration()
			damage(&registration)
			err := registration.Validate()
			if err == nil {
				t.Fatalf("a preregistration omitting %s was accepted", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the error does not name the omitted field %s: %v", name, err)
			}
		})
	}
}

func TestM24_219_SelectionAndConfirmationCannotShareData(t *testing.T) {
	registration := completePreregistration()
	registration.HeldOutCohort = registration.TuningCohort
	if err := registration.Validate(); err == nil {
		t.Fatal("a preregistration that selects and confirms on the same cohort was accepted")
	}

	single := completePreregistration()
	single.Repetitions = 1
	if err := single.Validate(); err == nil {
		t.Fatal("a single-run preregistration was accepted; it cannot separate effect from variance")
	}
}

func TestM24_220_HiddenResultsMayNotSelectACandidate(t *testing.T) {
	_, _, err := JudgeCandidate(completePreregistration(), nil, true, true)
	if err == nil {
		t.Fatal("a candidate selected using hidden-evaluator results was judged")
	}
	if !strings.Contains(err.Error(), "answer key") {
		t.Errorf("the refusal does not say why it matters: %v", err)
	}
}

func TestM24_220_AnUnregisteredCandidateCannotBeJudged(t *testing.T) {
	incomplete := completePreregistration()
	incomplete.StopRule = ""
	if _, _, err := JudgeCandidate(incomplete, nil, true, false); err == nil {
		t.Fatal("a candidate with no registered stop rule was judged")
	}
}

func TestM24_221_AnyRegressionInANamedDimensionRetiresTheCandidate(t *testing.T) {
	for _, dimension := range RegressionDimensions() {
		t.Run(dimension, func(t *testing.T) {
			outcome, reason, err := JudgeCandidate(
				completePreregistration(), map[string]bool{dimension: true}, true, false)
			if err != nil {
				t.Fatalf("judging failed: %v", err)
			}
			// The gate was met. It does not matter: a faster system that is less
			// correct is not a better system.
			if outcome != SelectionRetired {
				t.Fatalf("a candidate that regressed in %s was %q, want %q",
					dimension, outcome, SelectionRetired)
			}
			if !strings.Contains(reason, dimension) {
				t.Errorf("the reason does not name the regression: %q", reason)
			}
		})
	}
}

func TestM24_220_ACandidateIsRetainedOnlyOnItsPreregisteredGate(t *testing.T) {
	outcome, _, err := JudgeCandidate(completePreregistration(), nil, true, false)
	if err != nil {
		t.Fatalf("judging failed: %v", err)
	}
	if outcome != SelectionRetained {
		t.Errorf("a clean candidate that met its gate was %q, want %q", outcome, SelectionRetained)
	}

	outcome, reason, err := JudgeCandidate(completePreregistration(), nil, false, false)
	if err != nil {
		t.Fatalf("judging failed: %v", err)
	}
	if outcome != SelectionInconclusive {
		t.Errorf("a candidate that missed its gate was %q, want %q",
			outcome, SelectionInconclusive)
	}
	if !strings.Contains(reason, "preregistered gate") {
		t.Errorf("the reason does not cite the gate: %q", reason)
	}
}
