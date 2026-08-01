package dogfood

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestM24_191_MeasurementSetCoversEveryPerTaskNumber(t *testing.T) {
	if err := ValidateMeasures(); err != nil {
		t.Fatalf("declared measurements are not valid: %v", err)
	}
	for index, measure := range Measures() {
		if got, want := MeasureTodo(measure.ID), fmt.Sprintf("M24-%d", 191+index); got != want {
			t.Errorf("measure %q maps to %s, want %s", measure.ID, got, want)
		}
	}
	if MeasureTodo("invented-measure") != "" {
		t.Error("an undeclared measure was given a TODO")
	}
}

func TestM24_191_EveryMeasurementSaysWhatABareCountWouldHide(t *testing.T) {
	// A measurement without this is a number somebody will quote out of
	// context, which is the failure mode this whole block exists to prevent.
	for _, measure := range Measures() {
		if len(measure.Fields) < 2 {
			t.Errorf("measure %q records %d field(s); a single number has no denominator",
				measure.ID, len(measure.Fields))
		}
		if len(measure.WhyDenominatorsMatter) < 40 {
			t.Errorf("measure %q gives no real account of what a bare count hides: %q",
				measure.ID, measure.WhyDenominatorsMatter)
		}
	}
}

func TestM24_191_AcceptanceIsNotMeasuredByTheAgentAlone(t *testing.T) {
	fields := measureFields(t, MeasureAcceptance)
	for _, required := range []string{
		"hidden-acceptance", "independent-diff-review", "delayed-defects",
	} {
		if !slices.Contains(fields, required) {
			t.Errorf("acceptance does not record %q, so a self-graded pass would count",
				required)
		}
	}
}

func TestM24_193_CostIncludesTheHumanTimeTheRunConsumed(t *testing.T) {
	fields := measureFields(t, MeasureCost)
	if !slices.Contains(fields, "estimated-human-cost") {
		t.Error("cost omits human time, which is usually the larger number")
	}
}

func TestM24_196_ContextSelectionRecordsWhatWasActuallyNecessary(t *testing.T) {
	fields := measureFields(t, MeasureContext)
	for _, required := range []string{"files-changed", "files-independently-necessary"} {
		if !slices.Contains(fields, required) {
			t.Errorf("context selection omits %q, so a wide selection would look precise",
				required)
		}
	}
}

func TestM24_197_ApprovalsRecordTheRetrospectiveJudgement(t *testing.T) {
	fields := measureFields(t, MeasureApprovals)
	for _, required := range []string{
		"retrospectively-unnecessary", "retrospectively-too-broad",
	} {
		if !slices.Contains(fields, required) {
			t.Errorf("approvals omit %q, so a high grant rate would read as trust", required)
		}
	}
}

func TestM24_200_MemoryUseSeparatesRetrievalFromInfluence(t *testing.T) {
	fields := measureFields(t, MeasureMemoryUse)
	for _, required := range []string{"influence", "acceptance-outcome"} {
		if !slices.Contains(fields, required) {
			t.Errorf(
				"memory use omits %q, so an item retrieved and ignored would count as "+
					"memory working", required)
		}
	}
}

func TestM24_201_AtomUseRecordsTheCostSideToo(t *testing.T) {
	fields := measureFields(t, MeasureAtomUse)
	for _, required := range []string{"newly-admitted", "invalidated", "rejected"} {
		if !slices.Contains(fields, required) {
			t.Errorf("atom use omits %q, which is a cost, not a benefit", required)
		}
	}
}

func measureFields(t *testing.T, id MeasureID) []string {
	t.Helper()
	for _, measure := range Measures() {
		if measure.ID == id {
			return measure.Fields
		}
	}
	t.Fatalf("no measurement is declared for %q", id)
	return nil
}

func TestM24_202_TrackPlanIsolatesOneVariableAtATime(t *testing.T) {
	if err := ValidateTrackExecutions(); err != nil {
		t.Fatalf("the declared track plan is not valid: %v", err)
	}
	names := map[string]TrackExecution{}
	for _, execution := range TrackExecutions() {
		names[execution.Name] = execution
	}
	for _, name := range []string{"A", "B", "C", "D"} {
		if _, declared := names[name]; !declared {
			t.Fatalf("track %s is not declared", name)
		}
	}
	if names["A"].Baseline == "" {
		t.Error("track A names no baseline, so its numbers compare to nothing")
	}
	if names["B"].MemoryEnabled {
		t.Error("track B runs with memory enabled, so C measures nothing extra")
	}
	if !names["C"].MemoryEnabled || names["C"].VectorDiscoveryEnabled {
		t.Error("track C must isolate deterministic memory from vector discovery")
	}
}

func TestM24_205_ADeferredTrackRecordsWhatItsAbsenceForbids(t *testing.T) {
	var trackD TrackExecution
	for _, execution := range TrackExecutions() {
		if execution.Name == "D" {
			trackD = execution
		}
	}
	if trackD.Executed {
		t.Fatal("track D is marked executed; §0 defers adaptive policy until it is earned")
	}
	if len(trackD.ExcludedClaims) == 0 {
		t.Fatal("track D records nothing its absence forbids claiming")
	}

	// A deferral that records neither a trigger nor an exclusion is a deferral
	// nobody remembers, and the claims get made anyway.
	silent := trackD
	silent.ExcludedClaims = nil
	if err := silent.Validate(); err == nil {
		t.Error("a deferred track with no excluded claims was accepted")
	}
	untriggered := trackD
	untriggered.DeferralTrigger = ""
	if err := untriggered.Validate(); err == nil {
		t.Error("a deferred track with no authorisation trigger was accepted")
	}
}

func TestM24_204_VectorDiscoveryWithoutMemoryIsRefused(t *testing.T) {
	incoherent := TrackExecution{
		Name: "X", Todo: "M24-204",
		MemoryEnabled: false, VectorDiscoveryEnabled: true, Executed: true,
	}
	if err := incoherent.Validate(); err == nil {
		t.Fatal("a track that discovers among nothing was accepted")
	}
}

func TestM24_208_EverySeparationRecordsItsResidual(t *testing.T) {
	for _, source := range AllConfounders() {
		complete := Separation{Source: source, Method: "held constant", Residual: "run-to-run noise"}
		if err := complete.Validate(); err != nil {
			t.Errorf("a complete separation for %q was rejected: %v", source, err)
		}
		if err := (Separation{Source: source, Method: "held constant"}).Validate(); err == nil {
			t.Errorf(
				"a separation for %q with no residual was accepted; claiming a complete "+
					"separation is itself an unsupportable claim", source)
		}
		if err := (Separation{Source: source, Residual: "noise"}).Validate(); err == nil {
			t.Errorf("a separation for %q with no method was accepted", source)
		}
	}
	if err := (Separation{Source: "vibes", Method: "m", Residual: "r"}).Validate(); err == nil {
		t.Error("an undeclared confounding source was accepted")
	}
}

func TestM24_207_GettingFasterByGettingWorseIsNotImprovement(t *testing.T) {
	improving := MarginalTrend{TimeDeclined: true, CostDeclined: true}
	if !improving.Improving() {
		t.Error("a trend that got faster and cheaper was not reported as improving")
	}

	regressed := MarginalTrend{
		TimeDeclined: true, CostDeclined: true, ContextSizeDeclined: true,
		RepairRoundsDeclined: true, CorrectnessRegressed: true,
	}
	if regressed.Improving() {
		t.Error("a trend that improved every cost dimension while losing correctness was " +
			"reported as improving")
	}

	if (MarginalTrend{}).Improving() {
		t.Error("a flat trend was reported as improving")
	}
}

func completeComparison() ComparisonReport {
	separations := make([]Separation, 0, len(AllConfounders()))
	for _, source := range AllConfounders() {
		separations = append(separations, Separation{
			Source:   source,
			Method:   "held constant across arms and re-randomised across repetitions",
			Residual: "the portion attributable to unmodelled run-to-run variance",
		})
	}
	decisions := map[string]string{}
	for _, subject := range DecisionSubjects() {
		decisions[subject] = "continue"
	}
	return ComparisonReport{
		CorrectnessFirst:       true,
		IncludedFailedAttempts: true,
		MarginalTrend:          MarginalTrend{TimeDeclined: true},
		Separations:            separations,
		FinalRerunClean:        true,
		SuitesPassed:           true,
		ContractAgrees:         true,
		SecretScanSurfaces:     AllSecretSurfaces(),
		ScorecardComplete:      true,
		Inventory: []InventoryItem{
			{Summary: "expired approvals honoured on replay", Category: InventoryCorrectness},
			{Summary: "recovery prompt does not say what resuming will do",
				Category: InventoryBlocking},
			{Summary: "cold start dominated by migration", Category: InventorySpeed},
			{Summary: "context selection is wider than necessary", Category: InventoryCost},
		},
		Decisions:         decisions,
		PlanUpdates:       []string{"narrow §14 to the two graph modes that changed a decision"},
		UnresolvedBacklog: []string{"whether atom reuse clears §30's 20% bar at scale"},
	}
}

func TestM24_206_ACompleteComparisonIsAccepted(t *testing.T) {
	if err := completeComparison().Validate(); err != nil {
		t.Fatalf("a complete comparison was rejected: %v", err)
	}
}

func TestM24_206_SpeedIsNotComparedBeforeCorrectness(t *testing.T) {
	report := completeComparison()
	report.CorrectnessFirst = false
	err := report.Validate()
	if err == nil {
		t.Fatal("a comparison that led with speed was accepted")
	}
	if !strings.Contains(err.Error(), "correctness") {
		t.Errorf("the refusal does not say why: %v", err)
	}

	report = completeComparison()
	report.IncludedFailedAttempts = false
	if err := report.Validate(); err == nil {
		t.Fatal("a comparison that excluded failed attempts was accepted")
	}
}

func TestM24_208_AnUnseparatedConfounderBlocksTheConclusion(t *testing.T) {
	for _, omitted := range AllConfounders() {
		t.Run(string(omitted), func(t *testing.T) {
			report := completeComparison()
			report.Separations = slices.DeleteFunc(report.Separations,
				func(separation Separation) bool { return separation.Source == omitted })
			err := report.Validate()
			if err == nil {
				t.Fatalf("a comparison that never separated %q was accepted", omitted)
			}
			if !strings.Contains(err.Error(), string(omitted)) {
				t.Errorf("the error does not name the unseparated confounder: %v", err)
			}
		})
	}
}

func TestM24_209_TheFinalRerunAndSuitesAreRequired(t *testing.T) {
	for name, damage := range map[string]func(*ComparisonReport){
		"rerun not clean":  func(report *ComparisonReport) { report.FinalRerunClean = false },
		"suites failed":    func(report *ComparisonReport) { report.SuitesPassed = false },
		"contract differs": func(report *ComparisonReport) { report.ContractAgrees = false },
	} {
		t.Run(name, func(t *testing.T) {
			report := completeComparison()
			damage(&report)
			if err := report.Validate(); err == nil {
				t.Fatalf("a comparison with %s was accepted", name)
			}
		})
	}
}

func TestM24_211_EverySurfaceIsScannedAndNothingIsFound(t *testing.T) {
	for _, skipped := range AllSecretSurfaces() {
		t.Run(string(skipped), func(t *testing.T) {
			report := completeComparison()
			report.SecretScanSurfaces = slices.DeleteFunc(
				append([]SecretMarkerSurface{}, AllSecretSurfaces()...),
				func(surface SecretMarkerSurface) bool { return surface == skipped })
			err := report.Validate()
			if err == nil {
				t.Fatalf("a comparison that never scanned %q was accepted", skipped)
			}
			if !strings.Contains(err.Error(), string(skipped)) {
				t.Errorf("the error does not name the unscanned surface: %v", err)
			}
		})
	}

	found := completeComparison()
	found.SecretFindings = []LeakFinding{{}}
	if err := found.Validate(); err == nil {
		t.Fatal("a comparison that found seeded marker material was accepted")
	}
}

func TestM24_213_TheInventoryIsOrderedByRiskNotConvenience(t *testing.T) {
	report := completeComparison()
	// Cost items ahead of correctness items: the ordering that gets the easy
	// work done and leaves the dangerous work in the backlog.
	report.Inventory = []InventoryItem{
		{Summary: "cheaper context", Category: InventoryCost},
		{Summary: "expired approvals honoured on replay", Category: InventoryCorrectness},
	}
	if err := report.Validate(); err == nil {
		t.Fatal("an inventory ordered cost-first was accepted")
	}

	empty := completeComparison()
	empty.Inventory = nil
	if err := empty.Validate(); err == nil {
		t.Fatal("an empty refinement inventory was accepted")
	}

	unknown := completeComparison()
	unknown.Inventory = []InventoryItem{{Summary: "something", Category: "nice-to-have"}}
	if err := unknown.Validate(); err == nil {
		t.Fatal("an inventory with an undeclared category was accepted")
	}
}

func TestM24_214_EverySubsystemGetsAnExplicitDecision(t *testing.T) {
	for _, subject := range DecisionSubjects() {
		t.Run(subject, func(t *testing.T) {
			report := completeComparison()
			delete(report.Decisions, subject)
			err := report.Validate()
			if err == nil {
				t.Fatalf("a report with no decision for %q was accepted", subject)
			}
			if !strings.Contains(err.Error(), subject) {
				t.Errorf("the error does not name the undecided subject: %v", err)
			}
		})
	}

	for _, outcome := range DecisionOutcomes() {
		report := completeComparison()
		report.Decisions["vectors"] = outcome
		if err := report.Validate(); err != nil {
			t.Errorf("the permitted outcome %q was rejected: %v", outcome, err)
		}
	}

	report := completeComparison()
	report.Decisions["vectors"] = "revisit later"
	if err := report.Validate(); err == nil {
		t.Fatal("a decision outside the permitted set was accepted")
	}
}

func TestM24_215_TheTrialCannotAnswerEveryQuestionItRaised(t *testing.T) {
	report := completeComparison()
	report.UnresolvedBacklog = nil
	err := report.Validate()
	if err == nil {
		t.Fatal("a report that left nothing unresolved was accepted")
	}
	if !strings.Contains(err.Error(), "speculative architecture") {
		t.Errorf("the refusal does not say what the risk is: %v", err)
	}

	noUpdates := completeComparison()
	noUpdates.PlanUpdates = nil
	if err := noUpdates.Validate(); err == nil {
		t.Fatal("a trial that changed nothing in the plan was accepted")
	}
}
