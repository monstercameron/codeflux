package dogfood

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// MeasureID names one per-task measurement (M24-191..201).
type MeasureID string

const (
	MeasureAcceptance MeasureID = "acceptance"
	MeasureLatency    MeasureID = "latency"
	MeasureCost       MeasureID = "tokens-and-cost"
	MeasureForecast   MeasureID = "forecast-calibration"
	MeasureRework     MeasureID = "revisions-and-interventions"
	MeasureContext    MeasureID = "context-selection"
	MeasureApprovals  MeasureID = "approvals"
	MeasureRecovery   MeasureID = "recovery-outcomes"
	MeasureGraphUse   MeasureID = "graph-use"
	MeasureMemoryUse  MeasureID = "memory-retrieval-and-influence"
	MeasureAtomUse    MeasureID = "atom-reuse"
)

// AllMeasures returns every declared per-task measurement.
func AllMeasures() []MeasureID {
	return []MeasureID{
		MeasureAcceptance, MeasureLatency, MeasureCost, MeasureForecast,
		MeasureRework, MeasureContext, MeasureApprovals, MeasureRecovery,
		MeasureGraphUse, MeasureMemoryUse, MeasureAtomUse,
	}
}

// MeasureTodo maps a measurement to the TODO that requires it.
func MeasureTodo(measure MeasureID) string {
	index := slices.Index(AllMeasures(), measure)
	if index < 0 {
		return ""
	}
	return fmt.Sprintf("M24-%d", 191+index)
}

// Measure describes one measurement and what it must not omit.
type Measure struct {
	ID   MeasureID
	Todo string
	// Fields are the values recorded. Each is named so a partially recorded
	// measurement is reported rather than averaged over.
	Fields []string
	// WhyDenominatorsMatter states what a bare count would hide. Every
	// measurement here has a way of looking good by leaving something out.
	WhyDenominatorsMatter string
}

// Measures returns the declared per-task measurements (M24-191..201).
func Measures() []Measure {
	return []Measure{
		{
			MeasureAcceptance, "M24-191",
			[]string{
				"visible-acceptance", "hidden-acceptance", "independent-diff-review",
				"regressions", "delayed-defects",
			},
			"visible acceptance alone counts a task the agent graded itself on; the hidden " +
				"result and the delayed defects are what make it a real number",
		},
		{
			MeasureLatency, "M24-192",
			[]string{
				"to-forecast", "to-plan", "to-first-action", "to-first-diff",
				"to-validation", "to-review", "to-acceptance",
			},
			"total time hides where it went; a fast run that spent all of it before the " +
				"first diff feels nothing like a fast run",
		},
		{
			MeasureCost, "M24-193",
			[]string{
				"input-tokens", "cached-tokens", "output-tokens",
				"provider-cost", "tool-cost", "estimated-human-cost",
			},
			"provider cost alone omits the human time the run consumed, which is usually " +
				"the larger number",
		},
		{
			MeasureForecast, "M24-194",
			[]string{"p50-coverage", "p90-coverage", "absolute-error", "systematic-bias"},
			"an average error near zero can hide a forecast that is always wrong in " +
				"alternating directions",
		},
		{
			MeasureRework, "M24-195",
			[]string{
				"plan-revisions", "repair-rounds", "repeated-actions",
				"escalations", "manual-interventions",
			},
			"a task counted as one success may have taken four attempts, and the count of " +
				"attempts is what predicts the next task",
		},
		{
			MeasureContext, "M24-196",
			[]string{"files-selected", "files-changed", "files-independently-necessary"},
			"selecting a hundred files and changing three is not precision; the third " +
				"number is what says whether the selection was right",
		},
		{
			MeasureApprovals, "M24-197",
			[]string{
				"requested", "granted", "denied", "expired",
				"retrospectively-unnecessary", "retrospectively-too-broad",
			},
			"a high grant rate reads as trust and may mean the prompts were too vague to " +
				"refuse; the retrospective judgements are what distinguish them",
		},
		{
			MeasureRecovery, "M24-198",
			[]string{
				"checkpoints", "reconnects", "worker-recoveries",
				"coordinator-recoveries", "resumes",
			},
			"recovery that never happened proves nothing; the count says how much of the " +
				"recovery claim was actually exercised",
		},
		{
			MeasureGraphUse, "M24-199",
			[]string{
				"opens", "modes-used", "cross-navigations",
				"decisions-changed", "confusion-events", "rated-usefulness",
			},
			"opens alone measure curiosity; decisions-changed measures value, and " +
				"confusion measures cost",
		},
		{
			MeasureMemoryUse, "M24-200",
			[]string{
				"exact-candidates", "vector-candidates", "eligibility-decisions",
				"influence", "acceptance-outcome", "invalidations",
			},
			"retrieval count is not value; an item retrieved and ignored contributed " +
				"nothing, and counting it as memory working is the easiest self-deception here",
		},
		{
			MeasureAtomUse, "M24-201",
			[]string{
				"reused", "adapted", "rejected", "invalidated", "newly-admitted", "renamed",
			},
			"newly-admitted atoms are a cost, not a benefit; reuse against eligible tasks " +
				"is the number §30's kill criterion actually turns on",
		},
	}
}

// ValidateMeasures checks the set covers M24-191..201.
func ValidateMeasures() error {
	measures := Measures()
	if len(measures) != len(AllMeasures()) {
		return fmt.Errorf("%d measures declared for %d ids", len(measures), len(AllMeasures()))
	}
	todos := map[string]bool{}
	for index, measure := range measures {
		if measure.ID != AllMeasures()[index] {
			return fmt.Errorf("measure %d is %q, the order declares %q",
				index, measure.ID, AllMeasures()[index])
		}
		if len(measure.Fields) == 0 {
			return fmt.Errorf("measure %q records nothing", measure.ID)
		}
		if strings.TrimSpace(measure.WhyDenominatorsMatter) == "" {
			return fmt.Errorf(
				"measure %q does not say what a bare count would hide", measure.ID)
		}
		if todos[measure.Todo] {
			return fmt.Errorf("%s is claimed twice", measure.Todo)
		}
		todos[measure.Todo] = true
	}
	for number := 191; number <= 201; number++ {
		todo := fmt.Sprintf("M24-%d", number)
		if !todos[todo] {
			return fmt.Errorf("no measure claims %s", todo)
		}
	}
	return nil
}

// TrackExecution is one track's run configuration and status (M24-202..205).
type TrackExecution struct {
	Name string
	Todo string
	// Baseline names the comparison baseline, where one applies.
	Baseline string
	// MemoryEnabled and VectorDiscoveryEnabled are the variables the tracks
	// isolate.
	MemoryEnabled          bool
	VectorDiscoveryEnabled bool
	// Executed records whether the track was run.
	Executed bool
	// DeferralTrigger states what would authorise running a deferred track.
	DeferralTrigger string
	// ExcludedClaims are the conclusions this track's absence forbids.
	ExcludedClaims []string
}

// TrackExecutions returns the declared track plan (M24-202..205).
func TrackExecutions() []TrackExecution {
	return []TrackExecution{
		{
			Name: "A", Todo: "M24-202",
			Baseline: "the frozen strong coding-agent baseline",
			// Memory off: this track measures the agent loop, not what memory
			// contributes, and leaving it on would attribute one to the other.
			MemoryEnabled: false, VectorDiscoveryEnabled: false, Executed: true,
		},
		{
			Name: "B", Todo: "M24-203",
			MemoryEnabled: false, VectorDiscoveryEnabled: false, Executed: true,
		},
		{
			Name: "C", Todo: "M24-204",
			// Deterministic memory only. Enabling vectors here would mean any
			// difference from B could be either mechanism.
			MemoryEnabled: true, VectorDiscoveryEnabled: false, Executed: true,
		},
		{
			Name: "D", Todo: "M24-205",
			MemoryEnabled: true, VectorDiscoveryEnabled: true, Executed: false,
			DeferralTrigger: "measured evidence that adaptive policy improves outcome per " +
				"unit cost, from runs where the policy was the only variable",
			ExcludedClaims: []string{
				"any claim about adaptive policy",
				"any claim about vector discovery's contribution",
			},
		},
	}
}

// Validate rejects a track plan that could not support a comparison.
func (execution TrackExecution) Validate() error {
	switch {
	case strings.TrimSpace(execution.Name) == "":
		return errors.New("a track execution requires a name")
	case !strings.HasPrefix(execution.Todo, "M24-"):
		return fmt.Errorf("track %q cites %q, want an M24 TODO", execution.Name, execution.Todo)
	}
	// Vector discovery without memory is incoherent: there is nothing to
	// discover among.
	if execution.VectorDiscoveryEnabled && !execution.MemoryEnabled {
		return fmt.Errorf(
			"track %q enables vector discovery with memory disabled, which discovers "+
				"nothing", execution.Name)
	}
	// An unexecuted track must say what would authorise it AND what its
	// absence forbids claiming. Without the second, its absence is silently
	// forgotten and claims get made anyway.
	if !execution.Executed {
		if strings.TrimSpace(execution.DeferralTrigger) == "" {
			return fmt.Errorf("track %q is deferred with no authorisation trigger", execution.Name)
		}
		if len(execution.ExcludedClaims) == 0 {
			return fmt.Errorf(
				"track %q is deferred without recording what its absence forbids claiming",
				execution.Name)
		}
	}
	return nil
}

// ValidateTrackExecutions checks the plan covers M24-202..205 and isolates one
// variable at a time.
func ValidateTrackExecutions() error {
	executions := TrackExecutions()
	todos := map[string]bool{}
	for _, execution := range executions {
		if err := execution.Validate(); err != nil {
			return err
		}
		if todos[execution.Todo] {
			return fmt.Errorf("%s is claimed twice", execution.Todo)
		}
		todos[execution.Todo] = true
	}
	for number := 202; number <= 205; number++ {
		todo := fmt.Sprintf("M24-%d", number)
		if !todos[todo] {
			return fmt.Errorf("no track execution claims %s", todo)
		}
	}

	// B and C must differ in exactly one variable, or a difference between
	// them attributes to nothing in particular.
	var trackB, trackC TrackExecution
	for _, execution := range executions {
		switch execution.Name {
		case "B":
			trackB = execution
		case "C":
			trackC = execution
		}
	}
	differences := 0
	if trackB.MemoryEnabled != trackC.MemoryEnabled {
		differences++
	}
	if trackB.VectorDiscoveryEnabled != trackC.VectorDiscoveryEnabled {
		differences++
	}
	if differences != 1 {
		return fmt.Errorf(
			"tracks B and C differ in %d variables; a comparison that changes two things "+
				"attributes the difference to neither", differences)
	}
	return nil
}

// ConfoundingSource is something that could explain an apparent benefit
// (M24-208).
//
// Each is a real reason a trial has produced a flattering number before. A
// comparison that has not separated them is a comparison that has measured
// something other than the product.
type ConfoundingSource string

const (
	ConfoundModelVariance     ConfoundingSource = "model-variance"
	ConfoundBenchmarkLearning ConfoundingSource = "benchmark-learning"
	ConfoundEvaluatorLeakage  ConfoundingSource = "evaluator-leakage"
	ConfoundOperatorLearning  ConfoundingSource = "operator-learning"
)

// AllConfounders returns every source that must be separated.
func AllConfounders() []ConfoundingSource {
	return []ConfoundingSource{
		ConfoundModelVariance, ConfoundBenchmarkLearning,
		ConfoundEvaluatorLeakage, ConfoundOperatorLearning,
	}
}

// Separation is how one confounder was ruled out (M24-208).
type Separation struct {
	Source ConfoundingSource
	// Method is how it was separated.
	Method string
	// Residual is what remains unexplained after the separation. Recording it
	// is what keeps the conclusion honest: no separation is complete.
	Residual string
}

// Validate rejects a separation that does not actually separate.
func (separation Separation) Validate() error {
	if !slices.Contains(AllConfounders(), separation.Source) {
		return fmt.Errorf("unknown confounding source %q", separation.Source)
	}
	if strings.TrimSpace(separation.Method) == "" {
		return fmt.Errorf("confounder %q was not separated by any stated method",
			separation.Source)
	}
	if strings.TrimSpace(separation.Residual) == "" {
		return fmt.Errorf(
			"confounder %q records no residual; claiming a separation is complete is "+
				"itself a claim nobody can support", separation.Source)
	}
	return nil
}

// ComparisonReport is the dogfood conclusion (M24-206..215).
type ComparisonReport struct {
	// CorrectnessFirst records that correctness was compared before speed or
	// cost (M24-206).
	CorrectnessFirst bool
	// IncludedFailedAttempts records that cheap failed attempts, escalations,
	// interventions, and evaluator failures were counted (M24-206).
	IncludedFailedAttempts bool
	// MarginalTrend is the M24-207 observation.
	MarginalTrend MarginalTrend
	// Separations rule out the confounders (M24-208).
	Separations []Separation
	// FinalRerunClean records the M24-209 rerun from an untouched scaffold.
	FinalRerunClean bool
	// SuitesPassed and ContractAgrees record M24-210.
	SuitesPassed   bool
	ContractAgrees bool
	// SecretScanSurfaces are the surfaces scanned for markers (M24-211).
	SecretScanSurfaces []SecretMarkerSurface
	// SecretFindings are what the scan found. The required answer is none.
	SecretFindings []LeakFinding
	// Scorecard, Inventory, Decisions, and PlanUpdates are M24-212..215.
	ScorecardComplete bool
	Inventory         []InventoryItem
	Decisions         map[string]string
	PlanUpdates       []string
	UnresolvedBacklog []string
}

// MarginalTrend is whether the work got cheaper as it went (M24-207).
type MarginalTrend struct {
	TimeDeclined         bool
	CostDeclined         bool
	ContextSizeDeclined  bool
	RepairRoundsDeclined bool
	// CorrectnessRegressed disqualifies the whole observation: getting faster
	// by getting worse is not an improvement.
	CorrectnessRegressed bool
}

// Improving reports whether the trend is a genuine improvement.
func (trend MarginalTrend) Improving() bool {
	if trend.CorrectnessRegressed {
		return false
	}
	return trend.TimeDeclined || trend.CostDeclined ||
		trend.ContextSizeDeclined || trend.RepairRoundsDeclined
}

// InventoryItem is one entry in the refinement inventory (M24-213).
type InventoryItem struct {
	Summary string
	// Rank orders by correctness risk, then user-blocking friction, then
	// speed, then cost.
	Category InventoryCategory
	Priority int
}

// InventoryCategory orders the refinement inventory (M24-213).
type InventoryCategory string

const (
	InventoryCorrectness InventoryCategory = "correctness-risk"
	InventoryBlocking    InventoryCategory = "user-blocking-friction"
	InventorySpeed       InventoryCategory = "speed"
	InventoryCost        InventoryCategory = "cost"
)

// InventoryOrder returns the categories in priority order.
func InventoryOrder() []InventoryCategory {
	return []InventoryCategory{
		InventoryCorrectness, InventoryBlocking, InventorySpeed, InventoryCost,
	}
}

// DecisionSubjects are the things a decision must be recorded for (M24-214).
func DecisionSubjects() []string {
	return []string{
		"agent-loop", "graph", "atoms", "deterministic-memory", "vectors",
		"forecasting", "routing", "recovery", "frontend",
	}
}

// DecisionOutcomes are the permitted decisions (M24-214).
func DecisionOutcomes() []string {
	return []string{"continue", "narrow", "redesign", "defer", "kill"}
}

// Validate rejects a comparison that could not support its conclusion.
func (report ComparisonReport) Validate() error {
	// M24-206: correctness first, and every failed attempt counted. Comparing
	// speed first is how a faster-but-wrong system wins a benchmark.
	if !report.CorrectnessFirst {
		return errors.New(
			"correctness was not compared before speed or cost; comparing speed first is " +
				"how a faster and less correct system wins")
	}
	if !report.IncludedFailedAttempts {
		return errors.New(
			"failed cheap attempts, escalations, interventions, and evaluator failures " +
				"were excluded, which makes every remaining number an overstatement")
	}

	// M24-208: every confounder separated, with its residual recorded.
	separated := map[ConfoundingSource]bool{}
	for _, separation := range report.Separations {
		if err := separation.Validate(); err != nil {
			return err
		}
		separated[separation.Source] = true
	}
	for _, confounder := range AllConfounders() {
		if !separated[confounder] {
			return fmt.Errorf(
				"confounder %q was not separated; an observed benefit could be entirely "+
					"this", confounder)
		}
	}

	// M24-209, M24-210.
	if !report.FinalRerunClean {
		return errors.New("the final rerun from an untouched scaffold was not clean")
	}
	if !report.SuitesPassed {
		return errors.New("the complete visible and hidden suites did not pass")
	}
	if !report.ContractAgrees {
		return errors.New("the API and its OpenAPI description do not agree")
	}

	// M24-211: every surface scanned, and nothing found.
	scanned := map[SecretMarkerSurface]bool{}
	for _, surface := range report.SecretScanSurfaces {
		scanned[surface] = true
	}
	var unscanned []string
	for _, surface := range AllSecretSurfaces() {
		if !scanned[surface] {
			unscanned = append(unscanned, string(surface))
		}
	}
	if len(unscanned) > 0 {
		sort.Strings(unscanned)
		return fmt.Errorf("these surfaces were never scanned for seeded markers: %s",
			strings.Join(unscanned, ", "))
	}
	if len(report.SecretFindings) > 0 {
		return fmt.Errorf("seeded marker material was found on %d surface(s)",
			len(report.SecretFindings))
	}

	// M24-212, M24-213.
	if !report.ScorecardComplete {
		return errors.New("the final dogfood scorecard is incomplete")
	}
	if len(report.Inventory) == 0 {
		return errors.New("the refinement inventory is empty")
	}
	// Every category is checked before the ordering, so a single-item inventory
	// with an unknown category is still caught: the ordering loop alone never
	// looks at an item with no neighbour.
	for _, item := range report.Inventory {
		if slices.Index(InventoryOrder(), item.Category) < 0 {
			return fmt.Errorf("the inventory entry %q has unknown category %q",
				item.Summary, item.Category)
		}
	}
	for index := 1; index < len(report.Inventory); index++ {
		previous := slices.Index(InventoryOrder(), report.Inventory[index-1].Category)
		current := slices.Index(InventoryOrder(), report.Inventory[index].Category)
		if current < previous {
			return fmt.Errorf(
				"the inventory is not ordered by risk: %q follows %q",
				report.Inventory[index].Category, report.Inventory[index-1].Category)
		}
	}

	// M24-214: a decision for every subject, from the permitted set.
	for _, subject := range DecisionSubjects() {
		outcome, decided := report.Decisions[subject]
		if !decided {
			return fmt.Errorf("no decision was recorded for %q", subject)
		}
		if !slices.Contains(DecisionOutcomes(), outcome) {
			return fmt.Errorf("subject %q was decided %q, which is not a permitted outcome",
				subject, outcome)
		}
	}

	// M24-215: the plan takes only what the trial supports, and everything
	// else stays in the backlog rather than becoming architecture.
	if len(report.PlanUpdates) == 0 {
		return errors.New("no plan updates were recorded from the trial")
	}
	if len(report.UnresolvedBacklog) == 0 {
		return errors.New(
			"nothing was left unresolved, which means either the trial answered every " +
				"question it raised, or unresolved observations were converted into " +
				"speculative architecture")
	}
	return nil
}
